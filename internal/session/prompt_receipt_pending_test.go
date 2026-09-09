package session

import (
	"testing"
	"time"
)

// PendingSince is what `session output` needs to stop being misread as an
// answer to "did my message arrive?". These cases pin its one-sidedness: it
// may only ever add the claim "a turn is in flight", never the claim that
// nothing is pending, because the absence of a hook record is the normal state
// of a tool that writes none.

func TestPendingSince(t *testing.T) {
	prompt := time.Unix(1788983687, 0) // 19:54:47Z, when sec-backup-restore accepted
	older := time.Unix(1788983383, 0)  // the report the conductor read instead
	newer := time.Unix(1788983715, 0)  // the answer, once it existed

	tests := []struct {
		name           string
		receipt        PromptReceipt
		lastResponseAt time.Time
		want           bool
		wantAt         time.Time
	}{
		{
			name:           "prompt accepted after the visible response is the whole point",
			receipt:        sample(promptSubmitEvent, "s1", prompt, 0),
			lastResponseAt: older,
			want:           true,
			wantAt:         prompt,
		},
		{
			name:           "the answer has landed, nothing is pending",
			receipt:        sample(promptSubmitEvent, "s1", prompt, 0),
			lastResponseAt: newer,
			want:           false,
		},
		{
			name: "a finished turn says Stop, and Stop is not a pending prompt",
			// The distinction the whole receipt rests on: only
			// UserPromptSubmit means input was taken up.
			receipt:        sample("Stop", "s1", prompt, 0),
			lastResponseAt: older,
			want:           false,
		},
		{
			name:           "PermissionRequest is attention, not intake",
			receipt:        sample("PermissionRequest", "s1", prompt, 0),
			lastResponseAt: older,
			want:           false,
		},
		{
			name: "no hook file at all claims nothing",
			// The load-bearing negative: a tool that runs no hooks must
			// produce silence, not a denial.
			receipt:        PromptReceipt{},
			lastResponseAt: older,
			want:           false,
		},
		{
			name:           "an undatable response skips the comparison rather than guessing",
			receipt:        sample(promptSubmitEvent, "s1", prompt, 0),
			lastResponseAt: time.Time{},
			want:           true,
			wantAt:         prompt,
		},
		{
			name: "same second resolves to silence",
			// Claude's `ts` has one-second resolution, so a response written
			// in the same second as the prompt that produced it cannot be
			// ordered against it. Missing a notice costs a caller one look at
			// the target; inventing one tells them a finished turn is still
			// thinking.
			receipt:        sample(promptSubmitEvent, "s1", prompt, 0),
			lastResponseAt: prompt,
			want:           false,
		},
		{
			name:           "a record with no timestamp cannot date anything",
			receipt:        sample(promptSubmitEvent, "s1", time.Time{}, 0),
			lastResponseAt: older,
			want:           false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at, ok := tc.receipt.PendingSince(tc.lastResponseAt)
			if ok != tc.want {
				t.Fatalf("PendingSince() ok = %v, want %v", ok, tc.want)
			}
			if ok && !at.Equal(tc.wantAt) {
				t.Fatalf("PendingSince() at = %v, want %v", at, tc.wantAt)
			}
			if !ok && !at.IsZero() {
				t.Fatalf("PendingSince() returned %v alongside ok=false; a caller that ignores ok must not read a time", at)
			}
		})
	}
}
