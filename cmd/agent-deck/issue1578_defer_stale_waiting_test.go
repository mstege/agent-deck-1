package main

import "testing"

// ---------------------------------------------------------------------------
// `session send --defer-if-busy` held a message for its full 30m timeout and
// then dropped it, against targets that were sitting at an empty composer the
// whole time.
//
// Observed 2026-09-09 on a fleet of ~20 parallel sessions. The evidence was
// unambiguous at the moment of observation: the target's hook file read
// {"status":"waiting","event":"Stop"} — its foreground turn had ended eleven
// minutes earlier — while `agent-deck list --json` reported "running" and the
// pane showed `❯ ` with "1 shell still running" in the footer. Two send
// processes were hanging on that gate at the time, for 8 and 6 minutes.
//
// The gate consulted the hook only while it was FRESH (2 minutes). Past that it
// fell through to UpdateStatus, which deliberately promotes a Claude "waiting"
// to running while `run_in_background` work is pending, so the TUI stays green
// and fires no premature "finished" notification. Correct for a status colour;
// wrong for a delivery gate, and permanent — a lingering background shell keeps
// the promotion alive indefinitely, so the hold never ends on its own.
//
// The rule these cases pin: hook records are edges, and age is not decay. A
// turn-finished edge stays true until a newer edge replaces it; only a busy edge
// is ambiguous once stale, because Claude writes "running" once at
// UserPromptSubmit and nothing more for the rest of the turn.
// ---------------------------------------------------------------------------

func TestHookEdgeSettlesDelivery(t *testing.T) {
	tests := []struct {
		name       string
		hookStatus string
		fresh      bool
		want       bool
	}{
		{
			name:       "stale turn-finished edge settles it: the Stop hook fired and cannot un-fire",
			hookStatus: "waiting",
			fresh:      false,
			want:       true,
		},
		{
			name:       "stale idle edge settles it for the same reason",
			hookStatus: "idle",
			fresh:      false,
			want:       true,
		},
		{
			name:       "stale busy edge is ambiguous — a long turn writes nothing after its first record",
			hookStatus: "running",
			fresh:      false,
			want:       false,
		},
		{
			name:       "stale starting edge is ambiguous for the same reason",
			hookStatus: "starting",
			fresh:      false,
			want:       false,
		},
		{
			name:       "fresh busy edge settles it: hold",
			hookStatus: "running",
			fresh:      true,
			want:       true,
		},
		{
			name:       "fresh turn-finished edge settles it: deliver",
			hookStatus: "waiting",
			fresh:      true,
			want:       true,
		},
		{
			name:       "no hook record at all: nothing to settle, fall back to the heuristic",
			hookStatus: "",
			fresh:      false,
			want:       false,
		},
		{
			name:       "an empty record is not settled by being called fresh",
			hookStatus: "",
			fresh:      true,
			want:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hookEdgeSettlesDelivery(tc.hookStatus, tc.fresh); got != tc.want {
				t.Fatalf("hookEdgeSettlesDelivery(%q, %v) = %v, want %v",
					tc.hookStatus, tc.fresh, got, tc.want)
			}
		})
	}
}
