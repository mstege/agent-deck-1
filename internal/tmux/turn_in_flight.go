package tmux

import (
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// "Is a turn running right now?" answered from the pane BODY, not the title.
//
// The obvious place to look is the OSC pane title, and AnalyzePaneTitle already
// classifies a Braille frame there as TitleStateWorking. On Claude Code
// v2.1.266 that signal is simply not there any more: sampling every agent-deck
// pane every two seconds for 75 seconds (4484 samples across 118 panes,
// 2026-09-09) produced ZERO Braille characters and exactly one leading glyph —
// U+2733 '✳', the done marker — including on a session that was executing tool
// calls throughout the measurement.
//
// So the title cannot distinguish a running turn from a finished one on this
// version, and any check built on it is inert. The pane body can: Claude prints
// a live status line ("· Flibbertigibbeting… (9m 29s · ↓ 35.9k tokens)") and a
// "ctrl+c to interrupt" hint while, and only while, a turn is in flight. Those
// are exactly the BusyPatterns this package already ships for the tool
// (patterns.go), which makes this a second reader of an existing signal rather
// than a new heuristic.
// ---------------------------------------------------------------------------

var (
	claudeBusyOnce     sync.Once
	claudeBusyResolved *ResolvedPatterns
)

func claudeBusyPatterns() *ResolvedPatterns {
	claudeBusyOnce.Do(func() {
		if resolved, err := CompilePatterns(DefaultRawPatterns("claude")); err == nil {
			claudeBusyResolved = resolved
		}
	})
	return claudeBusyResolved
}

// claudeTurnInFlight reports whether ANSI-stripped pane content shows a Claude
// turn currently generating.
//
// Deliberately narrower than the status detector's equivalent: it consults only
// the authoritative busy patterns and mutates no tracker state, so a caller can
// ask the question without moving the state machine. It is used to keep a
// session marked as working, so every uncertain case answers false.
func claudeTurnInFlight(content string) bool {
	patterns := claudeBusyPatterns()
	if patterns == nil || content == "" {
		return false
	}
	// The same 25-line window the status detector uses: the live status line
	// sits just above the composer, and a wider window starts matching the
	// model's own prose about spinners and interrupts.
	recent := strings.Join(lastNLines(content, 25), "\n")
	for _, re := range patterns.BusyRegexps {
		if re.MatchString(recent) {
			return true
		}
	}
	// "ctrl+c to interrupt" is a status-bar string, and the model writes about
	// interrupting often enough that an unanchored match is a real false
	// positive source — hence the same last-3-lines context gate the detector
	// applies.
	lower := strings.ToLower(recent)
	statusBar := lastNLines(content, 3)
	for _, str := range patterns.BusyStrings {
		lowerStr := strings.ToLower(str)
		if !strings.Contains(lower, lowerStr) {
			continue
		}
		if strings.Contains(lowerStr, "interrupt") &&
			!hasInterruptBusyContext(statusBar, lowerStr, patterns.SpinnerChars) {
			continue
		}
		return true
	}
	return false
}

// TurnInFlight reports whether this Claude session is generating right now.
//
// It reads the same short-lived pane cache BackgroundWorkPending uses, so when
// both are called in one pass — which is exactly what the "waiting" branch of
// Instance.UpdateStatus does — the second costs no capture. A capture failure
// or a non-Claude tool answers false: the caller then behaves as it did before
// this signal existed, which is the safe direction for something that can only
// keep a session marked as working.
func (s *Session) TurnInFlight() bool {
	if !s.isClaudeTool() {
		return false
	}
	raw, err := s.CapturePane()
	if err != nil {
		return false
	}
	return claudeTurnInFlight(StripANSI(raw))
}
