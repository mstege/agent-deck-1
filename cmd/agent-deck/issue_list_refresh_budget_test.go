package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// ---------------------------------------------------------------------------
// `agent-deck list --json` ran into timeouts past 120s on a loaded machine
// while a direct SQLite read of the same rows answered instantly, so callers
// learned to bypass the CLI (observed 2026-09-09, ~20 parallel sessions).
//
// Measured cause, on an idle machine with 138 sessions: 1.3s for the listing
// against 12ms for `sqlite3 state.db "select ... from instances"`. Never more
// than ~5 tmux processes alive at once during the run, so the cost is not lock
// contention on the database and not parallel fan-out — it is one tmux
// subprocess round-trip per session, in sequence. Each round-trip is bounded at
// 3s on its own; nothing bounded the sequence.
// ---------------------------------------------------------------------------

func TestListRefreshBudgetIsGenerousEnoughForAHealthyPass(t *testing.T) {
	// The measured healthy pass is ~1.3s for 138 sessions. A budget near that
	// would truncate normal listings on a slightly slower machine; one far
	// above it stops being a bound at all.
	if listRefreshBudget < 5*time.Second {
		t.Errorf("budget %s would truncate healthy listings (a 138-session pass measured 1.3s)", listRefreshBudget)
	}
	if listRefreshBudget > 30*time.Second {
		t.Errorf("budget %s is too long to keep a dispatcher moving", listRefreshBudget)
	}
}

// A healthy listing must stay byte-identical to what it has always been: the
// remote change probe (#2177) compares these bytes to decide whether anything
// changed, so an unconditional new field would make every listing look dirty.
func TestHealthyListingCarriesNoStalenessField(t *testing.T) {
	instances := []*session.Instance{{
		ID:        "probe-1",
		Title:     "probe",
		Tool:      "shell",
		CreatedAt: time.Unix(1788959000, 0),
	}}
	out, err := buildListJSON("default", instances)
	if err != nil {
		t.Fatalf("buildListJSON: %v", err)
	}
	if strings.Contains(string(out), "status_stale") {
		t.Errorf("a refreshed listing must not carry status_stale:\n%s", out)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("listing is not valid JSON: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("want 1 session, got %d", len(parsed))
	}
	if _, ok := parsed[0]["status"]; !ok {
		t.Error("every session must still carry a status")
	}
}
