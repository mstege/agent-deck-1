package tmux

import "testing"

// ---------------------------------------------------------------------------
// Fixtures captured from live Claude Code v2.1.266 panes on 2026-09-09, in the
// states the "waiting" branch of Instance.UpdateStatus has to tell apart.
//
// The pane BODY is the signal because the pane TITLE is not: 4484 title samples
// across 118 agent-deck panes that day contained zero Braille frames and
// exactly one leading glyph, the done marker '✳' — on sessions that were
// executing tool calls throughout. Anything built on AnalyzePaneTitle's
// TitleStateWorking is inert on this version.
// ---------------------------------------------------------------------------

// A turn in flight: the live status line with its elapsed timer and token
// counter. This is the state the session was in when a PermissionRequest hook
// wrote "waiting" and the daemon reported it as finished.
const paneTurnRunning = `⏺ Reading the delivery path now.

· Flibbertigibbeting… (9m 29s · ↓ 35.9k tokens)
────────────────────────────────────────────────────────────────── joiva ─
❯
──────────────────────────────────────────────────────────────────────────
  mirco@example.com | Opus 5 | feature-joiva | $14.52 | v2.1.266
  ⏵⏵ bypass permissions on · 1 monitor · ← for agents`

// The same session after the turn really ended: the status line has become a
// past-tense summary, no ellipsis.
const paneTurnFinished = `⏺ Done — the concept is on version 3.

✻ Cogitated for 2m 32s · done 13:19
────────────────────────────────────────────────────────────────── joiva ─
❯
──────────────────────────────────────────────────────────────────────────
  mirco@example.com | Opus 5 | feature-joiva | $14.52 | v2.1.266
  ⏵⏵ bypass permissions on · ← for agents`

// A freshly started session: nothing above the composer at all.
const paneFreshPrompt = `
────────────────────────────────────────────────────────────────── sw-auth ─
❯
────────────────────────────────────────────────────────────────────────────
  mirco@example.com | Opus 5 | stackwell | main | $0 | v2.1.266
  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents`

func TestClaudeTurnInFlight(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"a live status line means the turn is running", paneTurnRunning, true},
		{"a past-tense summary means it finished", paneTurnFinished, false},
		{"a fresh prompt is not a running turn", paneFreshPrompt, false},
		{"empty content is not a running turn", "", false},
		{
			// The explicit busy string, which is what a turn without a
			// whimsical status line shows.
			name:    "the interrupt hint in the status bar counts",
			content: "⏺ working\n\n✻ Thinking…\n❯\n  ctrl+c to interrupt",
			want:    true,
		},
		{
			// The model writing ABOUT interrupting is not the status bar
			// saying it. This is the false positive the context gate exists
			// for, and a conductor summarising this very investigation would
			// trip it.
			name: "the model writing about the interrupt hint does not count",
			content: `⏺ The recovery presses ctrl+c to interrupt the target, which is
  the harm issue #1979 describes.

✻ Cogitated for 41s · done 16:02
────────────────────────────────────────────────────────── conductor-mnemo ─
❯
────────────────────────────────────────────────────────────────────────────
  mirco@example.com | Sonnet 5 | mnemo | $8.21 | v2.1.266`,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := claudeTurnInFlight(tc.content); got != tc.want {
				t.Fatalf("claudeTurnInFlight = %v, want %v", got, tc.want)
			}
		})
	}
}

// The signal is one-way: it may only keep a session marked as working, so a
// non-Claude session and a session with no readable pane must both answer
// false.
func TestTurnInFlightFailsSafe(t *testing.T) {
	shell := &Session{Name: "agentdeck_shellsession_aaaaaaaa"}
	if shell.TurnInFlight() {
		t.Error("a non-Claude session must never report a Claude turn in flight")
	}
}
