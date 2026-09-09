package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// writeHookRecord plants a hook status file for instanceID in an isolated data
// dir and returns the receipt agent-deck would sample from it. It goes through
// GetHooksDir rather than assembling the path itself so the test cannot drift
// from the layout the production reader uses.
func writeHookRecord(t *testing.T, instanceID, event string, ts time.Time) session.PromptReceipt {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	dir := session.GetHooksDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	record := map[string]interface{}{
		"status":     "running",
		"session_id": "agent-session",
		"event":      event,
		"ts":         ts.Unix(),
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal hook record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, instanceID+".json"), data, 0o600); err != nil {
		t.Fatalf("write hook record: %v", err)
	}
	return session.SamplePromptReceipt(instanceID)
}

// The case this exists for, end to end from the hook file: on 2026-09-09 a
// conductor read `session output` 20 seconds into its target's thinking gap,
// saw the previous turn's report, and concluded the delivery had silently
// failed. The delivery had succeeded. The notice is the edge that was missing.
func TestPendingPromptNotice_ReportsAnAcceptedPromptTheOutputPredates(t *testing.T) {
	accepted := time.Unix(1788983687, 0) // 19:54:47Z
	now := accepted.Add(20 * time.Second)
	receipt := writeHookRecord(t, "af5b776f-1786466949", "UserPromptSubmit", accepted)

	line, fields := pendingPromptNotice(receipt, "2026-09-09T19:49:43.000Z", now)

	if line == "" {
		t.Fatal("no notice for a prompt accepted after the visible response — this is the misread the notice exists to prevent")
	}
	if !strings.Contains(line, "(20s ago)") {
		t.Errorf("notice does not say how old the pending prompt is: %q", line)
	}
	if !strings.Contains(line, accepted.UTC().Format(time.RFC3339)) {
		t.Errorf("notice does not name the acceptance time: %q", line)
	}
	if fields["awaiting_response"] != true {
		t.Errorf("awaiting_response = %v, want true", fields["awaiting_response"])
	}
	if got := fields["prompt_accepted_at"]; got != accepted.UTC().Format(time.RFC3339) {
		t.Errorf("prompt_accepted_at = %v, want %v", got, accepted.UTC().Format(time.RFC3339))
	}
}

// The direction that must never regress. A finished turn writes Stop, and a
// notice on a finished turn would tell a caller to keep waiting for an answer
// that is already on screen — the same misdiagnosis with the sign flipped.
func TestPendingPromptNotice_SaysNothingWithoutAnAcceptedPrompt(t *testing.T) {
	answered := time.Unix(1788983715, 0)
	receipt := writeHookRecord(t, "af5b776f-1786466949", "Stop", answered)

	line, fields := pendingPromptNotice(receipt, "2026-09-09T19:49:43.000Z", answered.Add(time.Minute))
	if line != "" || fields != nil {
		t.Fatalf("claimed a pending prompt from a Stop record: line=%q fields=%v", line, fields)
	}
}

// A tool that writes no hook file must produce silence. Rendering "nothing is
// pending" from a missing file would turn the receipt's one-sidedness into a
// two-sided claim it cannot support.
func TestPendingPromptNotice_MissingHookFileClaimsNothing(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	line, fields := pendingPromptNotice(
		session.SamplePromptReceipt("no-such-instance"), "2026-09-09T19:49:43.000Z", time.Now())
	if line != "" || fields != nil {
		t.Fatalf("spoke without a hook record: line=%q fields=%v", line, fields)
	}
}

func TestParseResponseTimestamp(t *testing.T) {
	tests := []struct {
		in       string
		wantZero bool
	}{
		{"2026-09-09T19:57:40.108Z", false},
		{"2026-09-09T19:57:40Z", false},
		{"2026-09-09T21:57:40+02:00", false},
		// Not an error condition: ResponseOutput.Timestamp is documented as
		// Claude-only, so several tools leave it empty or unparseable. The
		// zero time means "do not compare", which PendingSince honours.
		{"", true},
		{"last", true},
		{"1788983860", true},
	}
	for _, tc := range tests {
		got := parseResponseTimestamp(tc.in)
		if got.IsZero() != tc.wantZero {
			t.Errorf("parseResponseTimestamp(%q).IsZero() = %v, want %v", tc.in, got.IsZero(), tc.wantZero)
		}
	}
}

// A clock that disagrees with the hook writer's must not make the notice look
// broken — "(-3s ago)" in a message whose whole job is to be believed.
func TestHumanizeSince_SkipsNegativeAndUnknownAges(t *testing.T) {
	base := time.Unix(1788983687, 0)
	if got := humanizeSince(base, base.Add(-3*time.Second)); got != "" {
		t.Errorf("humanizeSince with a clock behind the record = %q, want empty", got)
	}
	if got := humanizeSince(base, time.Time{}); got != "" {
		t.Errorf("humanizeSince with no now = %q, want empty", got)
	}
	if got := humanizeSince(base, base.Add(90*time.Second)); got != " (1m ago)" {
		t.Errorf("humanizeSince(90s) = %q, want \" (1m ago)\"", got)
	}
	if got := humanizeSince(base, base.Add(3*time.Hour+5*time.Minute)); got != " (3h5m ago)" {
		t.Errorf("humanizeSince(3h5m) = %q, want \" (3h5m ago)\"", got)
	}
}
