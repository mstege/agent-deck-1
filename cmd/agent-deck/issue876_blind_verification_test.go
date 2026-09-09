package main

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// Issue #876, load mode: the verification loop must not report a message as
// "dropped silently" when it never managed to observe the target at all.
//
// Observed 2026-09-09 on a fleet of ~20 parallel sessions on a saturated
// machine: `session send` reported "send dropped silently: no evidence of
// delivery after 50 checks (issue #876)" against panes that were sitting at an
// empty, ready composer, and `tmux send-keys` to the very same panes worked. On
// that path `CapturePaneFresh` and `GetStatus` are both bounded at 3s and both
// get SIGKILLed under load, so every branch that could set delivery evidence is
// skipped for the whole budget — and the error then asserts three observations
// ("never went active", "no marker appeared", "body was not visible") that were
// never made.
//
// The distinction is operationally load-bearing, not cosmetic: "dropped" invites
// a resend, and a resend against a target that did receive the message
// duplicates it (#1978) or, on the default path, Ctrl+Cs a live turn (#1979).
// ---------------------------------------------------------------------------

var errProbeKilled = errors.New("failed to capture pane: signal: killed")

func TestBlindVerificationIsNotReportedAsSilentDrop(t *testing.T) {
	mock := &mockSendRetryTarget{
		statuses:   []string{"waiting"},
		statusErrs: []error{errProbeKilled},
		panes:      []string{""},
		paneErrs:   []error{errProbeKilled},
	}
	delivery, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 8, checkDelay: 0, verifyDelivery: true,
	})
	if err == nil {
		t.Fatal("a send that could not be verified must still be an error")
	}
	if delivery == deliveryNoEvidence {
		t.Fatalf("blind verification reported as a silent drop: %v", err)
	}
	if delivery != deliveryUnobserved {
		t.Fatalf("delivery status: want %q, got %q", deliveryUnobserved, delivery)
	}
	// The operator reads the last line and acts on it. It must not send them
	// to resend into a target that may already hold the message.
	if strings.Contains(err.Error(), "dropped") {
		t.Errorf("error must not claim the message was dropped, got: %v", err)
	}
	if !strings.Contains(err.Error(), "before resending") {
		t.Errorf("error must warn against a blind resend, got: %v", err)
	}
}

// The blind path must not reach for the Ctrl+C-and-resend recovery either: a
// destructive branch needs positive evidence, and there is none here. This is
// the #1979 gate seen from the load side.
func TestBlindVerificationSendsNoInterruptAndNoResend(t *testing.T) {
	mock := &mockSendRetryTarget{
		statuses:   []string{"waiting"},
		statusErrs: []error{errProbeKilled},
		panes:      []string{""},
		paneErrs:   []error{errProbeKilled},
	}
	if _, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 30, checkDelay: 0, verifyDelivery: true,
	}); err == nil {
		t.Fatal("expected an unverified-delivery error")
	}
	if got := atomic.LoadInt32(&mock.sendCtrlCCalls); got != 0 {
		t.Errorf("blind loop sent %d Ctrl+C keys; want 0", got)
	}
	if got := atomic.LoadInt32(&mock.sendKeysCalls); got != 1 {
		t.Errorf("blind loop delivered %d times; want exactly 1", got)
	}
}

// A single successful observation is enough to make the loop competent to
// judge: from then on "no evidence" is a real statement about the target and
// must keep its historical #876 classification.
func TestOneSuccessfulObservationKeepsTheNoEvidenceVerdict(t *testing.T) {
	mock := &mockSendRetryTarget{
		statuses:   []string{"waiting", "waiting"},
		statusErrs: []error{errProbeKilled, nil},
		panes:      []string{"", ""},
		paneErrs:   []error{errProbeKilled, errProbeKilled},
	}
	delivery, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 6, checkDelay: 0, verifyDelivery: true,
	})
	if err == nil {
		t.Fatal("expected the #876 no-evidence error")
	}
	if delivery != deliveryNoEvidence {
		t.Fatalf("delivery status: want %q, got %q", deliveryNoEvidence, delivery)
	}
}

// The no-evidence message must report how many checks actually returned an
// observation, so "50 checks" can never again stand for "50 attempts that all
// failed to look".
func TestNoEvidenceErrorReportsObservedCheckCount(t *testing.T) {
	mock := &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{""}}
	_, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 4, checkDelay: 0, verifyDelivery: true,
	})
	if err == nil {
		t.Fatal("expected the #876 no-evidence error")
	}
	if !strings.Contains(err.Error(), "after 4 checks, 4 of which returned an observation") {
		t.Errorf("error should report observed/total checks, got: %v", err)
	}
}

// An unobserved delivery is never reported as submitted in --json.
func TestUnobservedIsNotSubmittedInJSON(t *testing.T) {
	fields := sendDeliveryResult{delivery: deliveryUnobserved}.jsonFields()
	if fields["submitted"] != false {
		t.Errorf("submitted: want false, got %v", fields["submitted"])
	}
	if fields["delivery"] != deliveryUnobserved {
		t.Errorf("delivery: want %q, got %v", deliveryUnobserved, fields["delivery"])
	}
}

// --- the receipt: a positive signal that does not come off the pane ---------

// The load case the receipt exists for: every pane capture and status probe
// fails, and the send is still reported as submitted, because the agent itself
// acknowledged the prompt.
func TestReceiptConfirmsDeliveryWhileEveryPaneProbeFails(t *testing.T) {
	mock := &mockSendRetryTarget{
		statuses:   []string{"waiting"},
		statusErrs: []error{errProbeKilled},
		panes:      []string{""},
		paneErrs:   []error{errProbeKilled},
	}
	delivery, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 8, checkDelay: 0, verifyDelivery: true,
		deliveryReceipt: func() bool { return true },
	})
	if err != nil {
		t.Fatalf("a receipted delivery must succeed, got: %v", err)
	}
	if delivery != deliverySubmitted {
		t.Fatalf("delivery status: want %q, got %q", deliverySubmitted, delivery)
	}
}

// A receipt ends the loop at once: no further Enter nudges into a target that
// has already taken the prompt up, and no route to the Ctrl+C recovery.
func TestReceiptStopsTheLoopWithoutFurtherKeys(t *testing.T) {
	mock := &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{""}}
	if _, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 30, checkDelay: 0, verifyDelivery: true,
		deliveryReceipt: func() bool { return true },
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&mock.sendEnterCalls); got != 0 {
		t.Errorf("sent %d Enter nudges after a receipt; want 0", got)
	}
	if got := atomic.LoadInt32(&mock.sendCtrlCCalls); got != 0 {
		t.Errorf("sent %d Ctrl+C keys after a receipt; want 0", got)
	}
}

// A receipt arriving in the race between the last check and classification is
// still proof, not a failure.
func TestLateReceiptIsHonouredAtClassificationTime(t *testing.T) {
	mock := &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{""}}
	calls := 0
	delivery, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 3, checkDelay: 0, verifyDelivery: true,
		deliveryReceipt: func() bool {
			calls++
			// Nothing during the budget; true only on the post-budget look.
			return calls > 3
		},
	})
	if err != nil {
		t.Fatalf("a late receipt must still succeed, got: %v", err)
	}
	if delivery != deliverySubmitted {
		t.Fatalf("delivery status: want %q, got %q", deliverySubmitted, delivery)
	}
}

// Absence of a receipt must never become a verdict of its own: with hooks
// silent, the loop's pane-derived classification has to be exactly what it was
// before the receipt existed.
func TestNoReceiptLeavesTheExistingVerdictsUntouched(t *testing.T) {
	never := func() bool { return false }

	blind := &mockSendRetryTarget{
		statuses: []string{"waiting"}, statusErrs: []error{errProbeKilled},
		panes: []string{""}, paneErrs: []error{errProbeKilled},
	}
	if got, _ := sendWithRetryTarget(blind, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 4, checkDelay: 0, verifyDelivery: true, deliveryReceipt: never,
	}); got != deliveryUnobserved {
		t.Errorf("blind target: want %q, got %q", deliveryUnobserved, got)
	}

	silent := &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{""}}
	if got, _ := sendWithRetryTarget(silent, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 4, checkDelay: 0, verifyDelivery: true, deliveryReceipt: never,
	}); got != deliveryNoEvidence {
		t.Errorf("observed but silent target: want %q, got %q", deliveryNoEvidence, got)
	}

	const msg = "eine Nachricht an die Session"
	held := &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{claudeComposer(msg)}}
	if got, _ := sendWithRetryTarget(held, msg, false, sendRetryOptions{
		maxRetries: 4, checkDelay: 0, verifyDelivery: true, deliveryReceipt: never,
	}); got != deliveryTypedNotSubmitted {
		t.Errorf("composer still holding: want %q, got %q", deliveryTypedNotSubmitted, got)
	}
}

// --- the message must not recommend the harm --------------------------------
//
// All three delivery-failure messages ended with advice to resend: #876 with
// "before retrying", and both #1793 variants with "Treat this as NOT delivered
// — the submitting Enter may have been swallowed". On 2026-09-09 all three
// variants of a wrong verdict were observed in one day — success reported where
// nothing arrived, and failure reported where everything arrived (a completed
// `/clear` under #876, and a message being processed under #1793). A verdict
// that is wrong in both directions must not carry an instruction, least of all
// the one that duplicates a delivered message. For a deletion order, following
// it is unrecoverable.

func TestNoFailureMessageRecommendsResending(t *testing.T) {
	errProbeKilled := errProbeKilled
	const msg = "eine Nachricht an die Session"

	cases := []struct {
		name string
		mock *mockSendRetryTarget
	}{
		{
			// #876: nothing observed.
			name: "no_evidence",
			mock: &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{""}},
		},
		{
			// The blind case.
			name: "unobserved",
			mock: &mockSendRetryTarget{
				statuses: []string{"waiting"}, statusErrs: []error{errProbeKilled},
				panes: []string{""}, paneErrs: []error{errProbeKilled},
			},
		},
		{
			// #1413: the composer still holds it.
			name: "typed_not_submitted",
			mock: &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{claudeComposer(msg)}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sendWithRetryTarget(tc.mock, msg, false, sendRetryOptions{
				maxRetries: 4, checkDelay: 0, verifyDelivery: true,
			})
			if err == nil {
				t.Fatal("expected a delivery failure")
			}
			text := err.Error()
			for _, forbidden := range []string{
				"before retrying",
				"Treat this as NOT delivered",
			} {
				if strings.Contains(text, forbidden) {
					t.Errorf("message still recommends resending (%q): %s", forbidden, text)
				}
			}
		})
	}
}

// The two verdicts that mean "we could not confirm" must say so in those words
// — a caller that reads only the first clause must not come away with
// "not delivered".
func TestUnconfirmedVerdictsSayUnconfirmed(t *testing.T) {
	const msg = "eine Nachricht an die Session"
	for _, tc := range []struct {
		name string
		mock *mockSendRetryTarget
	}{
		{"no_evidence", &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{""}}},
		{"typed", &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{"prior output\n" + msg + "\n"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sendWithRetryTarget(tc.mock, msg, false, sendRetryOptions{
				maxRetries: 4, checkDelay: 0, verifyDelivery: true,
			})
			if err == nil {
				t.Fatal("expected a delivery failure")
			}
			if !strings.Contains(err.Error(), "UNCONFIRMED") {
				t.Errorf("verdict must name itself unconfirmed, got: %v", err)
			}
			if !strings.Contains(err.Error(), "DO NOT resend") {
				t.Errorf("verdict must warn against a blind resend, got: %v", err)
			}
		})
	}
}

// --- the third variant: NOT fixed, and this records why ---------------------
//
// `conductor-stayplace` reported `✓ Sent message` three times in a row for
// messages that never arrived, leaving its composer holding
// `1. Yes1. Yes1. Yes` — one unsubmitted copy per "successful" send
// (2026-09-09). The mechanism is understood: the default path treats a bare
// "active" status as proof of its own submission, and an agent that was
// ALREADY working looks identical whether or not the keystrokes landed.
//
// Gating that on a pre-send baseline was implemented and reverted. It broke
// eight existing expectations, the plain happy path among them, because a
// single-status fixture cannot express "not active before, active after" — so
// the failures prove neither that the rule is wrong nor that it is safe. That
// distinction is the work, and shipping the gate on a guess would trade a rare
// false success for a false failure on every ordinary send.
//
// What IS in place: the receipt runs before the status branch on both paths,
// and it is the signal that actually distinguishes a busy target that took the
// message from a busy target that did not.

// A busy target with a receipt is reported submitted — the receipt, not the
// busyness, is what settles it.
func TestBusyTargetIsSettledByTheReceipt(t *testing.T) {
	mock := &mockSendRetryTarget{statuses: []string{"active"}, panes: []string{""}}
	delivery, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 6, checkDelay: 0, verifyDelivery: true,
		deliveryReceipt: func() bool { return true },
	})
	if err != nil {
		t.Fatalf("a receipted delivery to a busy target must succeed: %v", err)
	}
	if delivery != deliverySubmitted {
		t.Fatalf("delivery: want %q, got %q", deliverySubmitted, delivery)
	}
}

// The receipt is consulted BEFORE the status branch, so it decides even when
// "active" would have answered on its own. That ordering is what lets the
// known gap above be closed later without changing this contract.
func TestReceiptIsCheckedBeforeTheStatusBranch(t *testing.T) {
	mock := &mockSendRetryTarget{statuses: []string{"active"}, panes: []string{""}}
	calls := 0
	if _, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 6, checkDelay: 0, verifyDelivery: true,
		deliveryReceipt: func() bool { calls++; return true },
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("the receipt must be consulted once, on the first iteration, got %d calls", calls)
	}
}
