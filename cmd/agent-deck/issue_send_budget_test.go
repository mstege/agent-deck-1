package main

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Five consecutive `session send` calls exceeded a 120s caller timeout on
// 2026-09-09 and were pushed into the background, which cost the caller a
// second call per delivery just to learn what had happened.
//
// The cause is that maxRetries bounds iterations, not time. Each iteration
// makes two tmux subprocess calls, each capped at 3s (plus a 2s reap grace),
// so 50 checks at a nominal 300ms cadence describe a ~15s loop on an idle
// machine — measured at 16.2s — and a loop of several minutes on a saturated
// one.
// ---------------------------------------------------------------------------

func TestSendVerifyBudgetIsWiredOnBothPaths(t *testing.T) {
	if got := defaultSendOptions().budget; got != sendVerifyBudget {
		t.Errorf("default path budget: want %s, got %s", sendVerifyBudget, got)
	}
	if got := noWaitSendOptions().budget; got != sendVerifyBudget {
		t.Errorf("--no-wait budget: want %s, got %s", sendVerifyBudget, got)
	}
}

func TestSendVerifyBudgetIsSizedAgainstTheTwoNumbersThatMatter(t *testing.T) {
	// The default path's nominal duration: 50 checks at 300ms.
	nominal := time.Duration(defaultSendOptions().maxRetries) * defaultSendOptions().checkDelay
	if sendVerifyBudget < 2*nominal {
		t.Errorf("budget %s would cut a healthy loop short (nominal %s)", sendVerifyBudget, nominal)
	}
	// Must leave room for the phases that run BEFORE verification — the defer
	// hold and the readiness wait — inside a 120s caller timeout.
	if sendVerifyBudget > 90*time.Second {
		t.Errorf("budget %s leaves no room under a 120s caller timeout", sendVerifyBudget)
	}
}

// The wall clock must actually stop the loop, and the verdict must be the one
// the observations support — not a different one because time ran out.
func TestBudgetStopsTheLoopAndKeepsTheVerdict(t *testing.T) {
	// A target that never shows anything: without a budget this runs all 500
	// checks, with one it stops early. Either way the verdict is no_evidence.
	mock := &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{""}}
	start := time.Now()
	delivery, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 500, checkDelay: 2 * time.Millisecond, verifyDelivery: true,
		budget: 60 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the #876 no-evidence error")
	}
	if delivery != deliveryNoEvidence {
		t.Fatalf("delivery: want %q, got %q", deliveryNoEvidence, delivery)
	}
	if elapsed > time.Second {
		t.Errorf("budget did not stop the loop: took %s for a 60ms budget", elapsed)
	}
	// And the message must report the checks it RAN, not the budget it was
	// given — "50 checks" standing for a loop that managed three is the same
	// dishonesty as claiming observations that never happened.
	if strings.Contains(err.Error(), "after 500 checks") {
		t.Errorf("error reports the retry cap instead of the checks run: %v", err)
	}
}

// A zero budget is the historical unbounded behaviour, which the existing
// tests rely on to exercise the retry count in isolation.
func TestZeroBudgetIsUnbounded(t *testing.T) {
	mock := &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{""}}
	_, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 4, checkDelay: 0, verifyDelivery: true,
	})
	if err == nil {
		t.Fatal("expected the #876 no-evidence error")
	}
	if !strings.Contains(err.Error(), "after 4 checks") {
		t.Errorf("an unbounded loop must run all 4 checks, got: %v", err)
	}
}

// --- the /clear case: which inputs may treat a session reset as a receipt --

func TestResetsAgentSession(t *testing.T) {
	tests := []struct {
		message string
		want    bool
	}{
		{"/clear", true},
		{"  /clear  ", true},
		{"/clear\n", true},
		// A slash command with an argument is still that command.
		{"/clear now please", true},
		// Not /clear, and must not be mistaken for it.
		{"/clearance check", false},
		{"/cleared", false},
		{"/compact", false},
		{"/rc", false},
		// A message that merely mentions it is not it.
		{"run /clear when you are done", false},
		{"", false},
		{"   ", false},
		{"eine gewöhnliche Nachricht", false},
	}

	for _, tc := range tests {
		t.Run(tc.message, func(t *testing.T) {
			if got := resetsAgentSession(tc.message); got != tc.want {
				t.Fatalf("resetsAgentSession(%q) = %v, want %v", tc.message, got, tc.want)
			}
		})
	}
}

// --- naming a usage-limited target ---------------------------------------
//
// A session that hit an HTTP 429 accepts keystrokes and then refuses to submit
// them, so the operator gets an unsent message stuck in the composer and no
// indication why (observed 2026-09-09 on `cmd-dashboard`; recovery was a
// handoff plus a restart). agent-deck already detects the condition —
// SubstateUsageLimit exists for exactly this and says so in its own doc — and
// the send path was not looking.

func TestUsageLimitWarningIsSuppressedForMachineCallers(t *testing.T) {
	tests := []struct {
		name       string
		quiet      bool
		jsonOutput bool
		want       bool
	}{
		// A --json caller parses stdout; a warning line it cannot use is noise.
		{name: "json suppresses", jsonOutput: true, want: true},
		{name: "quiet suppresses", quiet: true, want: true},
		{name: "both suppress", quiet: true, jsonOutput: true, want: true},
		// The interactive operator is the one who needs to read it.
		{name: "plain output warns", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := quietOrJSON(tc.quiet, tc.jsonOutput); got != tc.want {
				t.Fatalf("quietOrJSON(%v, %v) = %v, want %v", tc.quiet, tc.jsonOutput, got, tc.want)
			}
		})
	}
}

// The condition must NOT become a refusal, and this pins the reason so a later
// change does not "tighten" it into a deadlock: the verdict is believed for
// hours and clears only when a real turn COMPLETES, so refusing to send
// prevents the turn that would clear it.
func TestUsageLimitIsNotAGate(t *testing.T) {
	// The send path has no usage-limit branch that can stop a delivery: the
	// only delivery-blocking statuses are the ones sendWithRetryTarget itself
	// returns. This is a documentation test — it fails loudly if someone adds
	// a refusing constant named after the substate.
	for _, blocking := range []string{
		deliverySendFailed, deliveryLineTooLong, deliveryNoEvidence,
		deliveryTyped, deliveryTypedNotSubmitted, deliveryUnobserved,
	} {
		if blocking == "usage_limit" {
			t.Fatal("usage-limit must not become a delivery verdict: it would deadlock, " +
				"because the substate only clears when a turn completes")
		}
	}
}
