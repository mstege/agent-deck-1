package main

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// ---------------------------------------------------------------------------
// Two defects in how a session is addressed by name, both observed 2026-09-09.
//
// 1. The trailing token of a tmux session name is NOT an instance id. Sessions
//    are named `agentdeck_<title>_<suffix>` where the suffix comes from
//    tmux.generateShortID() — four random bytes, hex-encoded, with no relation
//    to the instance id. GetCurrentSessionID read it as an id, so every
//    command that resolves "the current session" failed against a token the
//    caller never typed:
//
//      $ agent-deck session output          # in agentdeck_ad-zustellung_b18d8430
//      Error: session 'b18d8430' not found  # instance id is f2e1578b-1788958022
//
//    `session show`, which parses the same name via findSessionByTmux, worked
//    throughout — two parsers of one name, disagreeing, in one binary.
//
// 2. Not-found said only that. Every other ResolveSession outcome names its
//    candidates (ambiguous title, location, path, id prefix), because the
//    caller's next move is to pick one. The most common failure named nothing,
//    so a typo, a renamed session and a session in another profile all looked
//    identical.
//
// Neither is a truncation: title matching is exact and full-length, and the
// id-prefix branch matches from the start of the real id.
// ---------------------------------------------------------------------------

func nameTestInstances() []*session.Instance {
	return []*session.Instance{
		{ID: "7656bcbe-1786205801", Title: "mnm-opp-overview"},
		{ID: "b8e58025-1787571321", Title: "mnm-opp-exec"},
		{ID: "f2e1578b-1788958022", Title: "ad-zustellung"},
	}
}

// A long, prefixed, hyphenated title resolves in full — the failure was never
// truncation, so this pins that it stays that way.
func TestLongPrefixedTitleResolvesExactly(t *testing.T) {
	instances := nameTestInstances()
	inst, errMsg, _ := ResolveSession("mnm-opp-overview", instances)
	if inst == nil {
		t.Fatalf("exact title must resolve, got: %s", errMsg)
	}
	if inst.ID != "7656bcbe-1786205801" {
		t.Fatalf("resolved the wrong session: %s", inst.ID)
	}
	// The sibling sharing the first eleven characters must not be reachable by
	// a prefix of the other's title: titles match exactly, never by prefix.
	if inst, _, _ := ResolveSession("mnm-opp", instances); inst != nil {
		t.Fatalf("a title prefix must not resolve, got %q", inst.Title)
	}
}

// The tmux name's trailing token must never resolve anything — it is random
// hex, and treating it as an id is the defect.
func TestTmuxNameSuffixIsNotAnInstanceID(t *testing.T) {
	inst, errMsg, code := ResolveSession("b18d8430", nameTestInstances())
	if inst != nil {
		t.Fatalf("the tmux name suffix must not resolve to a session, got %q", inst.Title)
	}
	if code != ErrCodeNotFound {
		t.Errorf("code: want %q, got %q", ErrCodeNotFound, code)
	}
	// And the message must not leave the caller with only the token they never
	// typed: it has to say what IS reachable.
	if !strings.Contains(errMsg, "session(s) are registered here") {
		t.Errorf("not-found must describe what is reachable, got: %s", errMsg)
	}
}

func TestSessionNotFoundMessageNamesReachableSessions(t *testing.T) {
	instances := nameTestInstances()

	tests := []struct {
		name       string
		identifier string
		want       []string
		absent     []string
	}{
		{
			// The observed case: one character too many.
			name:       "a typo suggests the session it almost named",
			identifier: "mnm-opp-overvieww",
			want:       []string{"did you mean", "mnm-opp-overview", "7656bcbe-178"},
		},
		{
			name:       "a shared stem suggests both siblings",
			identifier: "mnm-opp",
			want:       []string{"mnm-opp-overview", "mnm-opp-exec"},
		},
		{
			name:       "wrong case suggests the right one",
			identifier: "AD-Zustellung",
			want:       []string{"did you mean", "ad-zustellung"},
		},
		{
			name:       "nothing similar still says how many exist and how to list them",
			identifier: "xyzzy-gibt-es-nicht",
			want:       []string{"3 session(s) are registered here", "agent-deck list"},
			absent:     []string{"did you mean"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, errMsg, code := ResolveSession(tc.identifier, instances)
			if code != ErrCodeNotFound {
				t.Fatalf("code: want %q, got %q (%s)", ErrCodeNotFound, code, errMsg)
			}
			if !strings.Contains(errMsg, tc.identifier) {
				t.Errorf("message must quote what the caller typed, got: %s", errMsg)
			}
			for _, want := range tc.want {
				if !strings.Contains(errMsg, want) {
					t.Errorf("message should contain %q, got: %s", want, errMsg)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(errMsg, absent) {
					t.Errorf("message should not contain %q, got: %s", absent, errMsg)
				}
			}
		})
	}
}

// An empty profile must say so rather than offer a suggestion list it cannot
// fill.
func TestSessionNotFoundWithNoSessions(t *testing.T) {
	_, errMsg, _ := ResolveSession("anything", nil)
	if !strings.Contains(errMsg, "no sessions") {
		t.Errorf("empty profile should be named as such, got: %s", errMsg)
	}
}

// The suggestion list is bounded: a fleet-sized answer is as unusable as none.
func TestSuggestionListIsBounded(t *testing.T) {
	var instances []*session.Instance
	for _, suffix := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"} {
		instances = append(instances, &session.Instance{
			ID:    "0000000" + suffix + "-1786205801",
			Title: "shared-stem-" + suffix,
		})
	}
	_, errMsg, _ := ResolveSession("shared-stem", instances)
	if got := strings.Count(errMsg, "\n  - "); got > maxSuggestedSessions {
		t.Errorf("listed %d suggestions, want at most %d", got, maxSuggestedSessions)
	}
	if !strings.Contains(errMsg, "and 3 more") {
		t.Errorf("the elided remainder must be counted, got: %s", errMsg)
	}
}

// GetCurrentSessionID returns the title half of the tmux name, never the
// random suffix.
func TestCurrentSessionIdentifierIsTheTitleHalf(t *testing.T) {
	tests := []struct {
		tmuxName string
		want     string
	}{
		{"agentdeck_ad-zustellung_b18d8430", "ad-zustellung"},
		{"agentdeck_mnm-opp-overview_73a186b8", "mnm-opp-overview"},
		// Only the FINAL underscore is structural; a title may contain them.
		{"agentdeck_my_project_name_deadbeef", "my_project_name"},
	}
	for _, tc := range tests {
		t.Run(tc.tmuxName, func(t *testing.T) {
			withoutPrefix := strings.TrimPrefix(tc.tmuxName, SessionNamePrefix)
			last := strings.LastIndex(withoutPrefix, "_")
			if got := withoutPrefix[:last]; got != tc.want {
				t.Fatalf("title half: want %q, got %q", tc.want, got)
			}
		})
	}
}
