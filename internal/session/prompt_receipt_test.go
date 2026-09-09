package session

import (
	"testing"
	"time"
)

// A receipt is a transition, never a snapshot. These cases pin the direction of
// each rule: a miss costs a fallback to the pane signals, a false positive
// certifies a message that was never delivered.

func sample(event, sid string, ts time.Time, seq uint64) PromptReceipt {
	return PromptReceipt{present: true, event: event, sessionID: sid, updatedAt: ts, sequence: seq}
}

func TestAcceptedSince(t *testing.T) {
	t0 := time.Unix(1788958600, 0)
	t1 := time.Unix(1788958608, 0)

	tests := []struct {
		name   string
		before PromptReceipt
		now    PromptReceipt
		want   bool
	}{
		{
			name:   "submit record newer than the pre-send stop record is our receipt",
			before: sample("Stop", "s1", t0, 0),
			now:    sample("UserPromptSubmit", "s1", t1, 0),
			want:   true,
		},
		{
			name:   "the pre-send submit record itself is not a receipt",
			before: sample("UserPromptSubmit", "s1", t0, 0),
			now:    sample("UserPromptSubmit", "s1", t0, 0),
			want:   false,
		},
		{
			name:   "a stop record is never a receipt, however new",
			before: sample("UserPromptSubmit", "s1", t0, 0),
			now:    sample("Stop", "s1", t1, 0),
			want:   false,
		},
		{
			name:   "SessionStart is not a receipt: the session came up, it took no prompt",
			before: PromptReceipt{},
			now:    sample("SessionStart", "s1", t1, 0),
			want:   false,
		},
		{
			name:   "PermissionRequest is not a receipt",
			before: sample("Stop", "s1", t0, 0),
			now:    sample("PermissionRequest", "s1", t1, 0),
			want:   false,
		},
		{
			name:   "a submit record in a file that did not exist before is ours",
			before: PromptReceipt{},
			now:    sample("UserPromptSubmit", "s1", t1, 0),
			want:   true,
		},
		{
			name:   "an absent record now is never a receipt",
			before: sample("Stop", "s1", t0, 0),
			now:    PromptReceipt{},
			want:   false,
		},
		{
			name:   "a new agent session id makes the record ours (/clear, restart)",
			before: sample("UserPromptSubmit", "s1", t1, 0),
			now:    sample("UserPromptSubmit", "s2", t1, 0),
			want:   true,
		},
		{
			name:   "same second, same session, no sequence: tie resolves to no receipt",
			before: sample("Stop", "s1", t1, 0),
			now:    sample("UserPromptSubmit", "s1", t1, 0),
			want:   false,
		},
		{
			name:   "a sequence breaks the same-second tie",
			before: sample("Stop", "s1", t1, 41),
			now:    sample("UserPromptSubmit", "s1", t1, 42),
			want:   true,
		},
		{
			name:   "a stale sequence is not a receipt even with a newer timestamp",
			before: sample("Stop", "s1", t0, 42),
			now:    sample("UserPromptSubmit", "s1", t1, 42),
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.now.AcceptedSince(tc.before); got != tc.want {
				t.Fatalf("AcceptedSince = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSamplePromptReceiptMissingInstanceIsZero(t *testing.T) {
	if got := SamplePromptReceipt(""); got.present {
		t.Fatalf("empty instance id must yield an absent sample, got %+v", got)
	}
	if SamplePromptReceipt("").AcceptedSince(PromptReceipt{}) {
		t.Fatal("an absent sample must never read as a receipt")
	}
}
