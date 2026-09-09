package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// An unrecognised command used to fall through to the interactive TUI launch,
// because the dispatch switch had no default. Outside a session that opens a
// full-screen UI in answer to a typo; inside one it printed
//
//	Error: Cannot launch the agent-deck TUI inside an agent-deck session.
//	This would create a recursive nested session.
//
// which names a cause unrelated to the caller's mistake. Observed 2026-09-09
// while looking for the accounts listing; `agent-deck account` — the singular
// of a real command — reproduces it in one word, and so does any caller that
// passes a whole command line as a single argument.
//
// The same shape one level down: `agent-deck session list` does not exist, and
// its rejection ignored --json, so a machine caller got human usage text on
// stdout and `json.load` failed with "Expecting value: line 1 column 1" — a
// message that is indistinguishable from an empty result.
// ---------------------------------------------------------------------------

func TestNearestCommand(t *testing.T) {
	tests := []struct {
		token string
		want  string
	}{
		// The observed typo.
		{"account", "accounts"},
		// The other direction: a plural of a singular command.
		{"sessions", "session"},
		{"lists", "list"},
		// Exact commands are their own nearest match, which keeps the
		// suggestion honest if one is ever reached here.
		{"list", "list"},
		// Ambiguous: `re` matches remove, rename, remote and remote-agent.
		// One of those is destructive and a tie would be resolved by Go's
		// randomised map order, so nothing is suggested.
		{"re", ""},
		{"rem", ""},
		// Nothing close at all.
		{"hurzelpfurz", ""},
		{"", ""},
		// Case is not the caller's problem.
		{"ACCOUNT", "accounts"},
	}

	for _, tc := range tests {
		t.Run(tc.token, func(t *testing.T) {
			if got := nearestCommand(tc.token); got != tc.want {
				t.Fatalf("nearestCommand(%q) = %q, want %q", tc.token, got, tc.want)
			}
		})
	}
}

// A suggestion is what the caller runs next, so a wrong one is worse than
// none — especially for a destructive command.
func TestNearestCommandDoesNotGuessAcrossDestructiveCommands(t *testing.T) {
	for _, token := range []string{"rename", "renam", "ren"} {
		if got := nearestCommand(token); got == "remove" || got == "rm" {
			t.Errorf("nearestCommand(%q) suggested the destructive %q", token, got)
		}
	}
	// And the reverse.
	for _, token := range []string{"remov", "remove"} {
		if got := nearestCommand(token); got == "rename" || got == "mv" {
			t.Errorf("nearestCommand(%q) suggested %q", token, got)
		}
	}
}

// Every subcommand this map names must be a real top-level command, or the
// error sends the caller to a second failure.
func TestSessionCommandElsewherePointsAtRealCommands(t *testing.T) {
	for sub, suggestion := range sessionCommandElsewhere {
		fields := strings.Fields(suggestion)
		if len(fields) < 2 || fields[0] != "agent-deck" {
			t.Errorf("%q: suggestion %q is not an agent-deck invocation", sub, suggestion)
			continue
		}
		if !commandRegistry[fields[1]] {
			t.Errorf("%q: suggested command %q is not in commandRegistry", sub, fields[1])
		}
	}
	// The one that actually cost time must be covered.
	if _, ok := sessionCommandElsewhere["list"]; !ok {
		t.Error("`session list` must name `agent-deck list`")
	}
}

// The registry is what decides "unknown", so a command the switch handles but
// the registry omits would be rejected as unknown. Spot-check the ones the
// findings touched.
func TestCommandRegistryCoversTheDispatchedCommands(t *testing.T) {
	for _, cmd := range []string{
		"accounts", "list", "session", "add", "launch", "remove", "rename",
		"status", "inbox", "worktree", "agents", "agent", "hook-handler",
	} {
		if !commandRegistry[cmd] {
			t.Errorf("commandRegistry is missing %q, which the dispatcher handles", cmd)
		}
	}
}
