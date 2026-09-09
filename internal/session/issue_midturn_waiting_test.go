package session

import "testing"

// ---------------------------------------------------------------------------
// A Claude session was reported as "waiting" — orange, attention-needed, and
// notified to its parent — while its pane showed a live spinner and a climbing
// token counter. Seven times in one day on one session (2026-09-09, `joiva`),
// each one spending an operator's attention on a session that needed nothing.
//
// The record is written correctly and read wrongly, because "waiting" answers
// two different questions at once. The hook handler maps THREE Claude events to
// it (cmd/agent-deck/hook_handler.go):
//
//	Stop                              -> the turn ended, nothing more without you
//	PermissionRequest                 -> something MID-TURN needs you right now
//	Notification(permission_prompt|…) -> same
//
// Under `--dangerously-skip-permissions` the second and third have no force at
// all: the agent does not stop for permission, so it keeps generating while a
// fresh "waiting" record stands for the whole hookFastPathWindow. The observed
// case: `joiva` (`⏵⏵ bypass permissions on`), PermissionRequest at 14:42:27
// wrote "waiting", the transition daemon emitted running→waiting to the parent
// two seconds later, and the pane went on working.
//
// This is the mirror image of the `--defer-if-busy` defect, not the same code:
// there a CORRECT Stop edge was reinterpreted downstream as running, so a
// finished turn read as busy. Here a WRONG edge is written upstream, so a
// running turn reads as finished. One status field, two questions, two
// directions.
// ---------------------------------------------------------------------------

func TestClaudeWaitingHookStatus(t *testing.T) {
	tests := []struct {
		name             string
		turnStillRunning bool
		bgWorkPending    bool
		acknowledged     bool
		want             Status
	}{
		{
			// The defect. A mid-turn permission event wrote "waiting" while the
			// spinner was still turning; motion is proof the agent is not
			// waiting for anyone.
			name:             "a live spinner outranks the waiting record",
			turnStillRunning: true,
			want:             StatusRunning,
		},
		{
			// Even after the user has looked: they looked at a session that is
			// working, which does not make it finished.
			name:             "a live spinner outranks acknowledgement too",
			turnStillRunning: true,
			acknowledged:     true,
			want:             StatusRunning,
		},
		{
			// The genuine turn-end case must be untouched: after Stop the
			// spinner is gone, and this is the state that has to stay orange so
			// the operator sees the session needs them.
			name: "a finished turn with nothing pending still waits",
			want: StatusWaiting,
		},
		{
			// The pre-existing promotion, unchanged.
			name:          "background work keeps a finished turn green",
			bgWorkPending: true,
			want:          StatusRunning,
		},
		{
			name:          "background work outranks acknowledgement, as before",
			bgWorkPending: true,
			acknowledged:  true,
			want:          StatusRunning,
		},
		{
			// Seen and finished: grey, not orange.
			name:         "an acknowledged finished turn is idle",
			acknowledged: true,
			want:         StatusIdle,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := claudeWaitingHookStatus(tc.turnStillRunning, tc.bgWorkPending, tc.acknowledged)
			if got != tc.want {
				t.Fatalf("claudeWaitingHookStatus(turn=%v, bg=%v, ack=%v) = %v, want %v",
					tc.turnStillRunning, tc.bgWorkPending, tc.acknowledged, got, tc.want)
			}
		})
	}
}

// Both promotions are one-way. Absence of evidence — a cold pane cache, a
// failed capture — must leave the verdict exactly where it was before either
// signal existed, or a machine too busy to read a pane would start reporting
// every working session as finished.
func TestWaitingVerdictIsUnchangedWithoutEvidence(t *testing.T) {
	for _, acknowledged := range []bool{false, true} {
		withEvidence := claudeWaitingHookStatus(false, false, acknowledged)
		want := StatusWaiting
		if acknowledged {
			want = StatusIdle
		}
		if withEvidence != want {
			t.Fatalf("no evidence, acknowledged=%v: got %v, want %v", acknowledged, withEvidence, want)
		}
	}
}
