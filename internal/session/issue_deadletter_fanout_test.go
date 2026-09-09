package session

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// One parked dead-letter record appeared in FIVE conductors' drains at once.
//
// Observed 2026-09-09: a record for `perm-probe` (reason `child_removed`, and
// no target at all because the child had been created --no-parent) made
// `inbox drain` warn and exit non-zero for buildbrain, joiva, mnemo,
// family-brain and stayplace. Each of them had to dismiss it as "not my area"
// and escalate, and each of their heartbeat drains looked broken meanwhile.
//
// The cause was that the drain counted the whole dead-letter DIRECTORY. A
// dead-lettered record either names the parent it failed to reach — in which
// case exactly that parent's drain should surface it — or it names nobody, and
// then it belongs to the operator and to no conductor at all.
// ---------------------------------------------------------------------------

// parkDeadLetterForTest puts one raw JSONL line into the dead-letter store under the
// test's isolated HOME.
func parkDeadLetterForTest(t *testing.T, childID, line string) {
	t.Helper()
	if err := os.MkdirAll(DeadLetterDir(), 0o755); err != nil {
		t.Fatalf("mkdir dead-letter dir: %v", err)
	}
	path := filepath.Join(DeadLetterDir(), childID+".jsonl")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write dead-letter record: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
}

func TestDeadLetterCountsAreAttributedToOneTarget(t *testing.T) {
	// The observed record: no target_session_id at all.
	parkDeadLetterForTest(t, "60815c78-1788962451",
		`{"child_session_id":"60815c78-1788962451","child_title":"perm-probe",`+
			`"profile":"permtest","from_status":"running","to_status":"error",`+
			`"dead_letter_reason":"child_removed","fp":"903808cd"}`)
	// And one that does name a parent.
	parkDeadLetterForTest(t, "aaaa1111-1700000000",
		`{"child_session_id":"aaaa1111-1700000000","child_title":"real-child",`+
			`"target_session_id":"parent-one","target_kind":"parent",`+
			`"from_status":"running","to_status":"error","fp":"deadbeef"}`)

	counts, err := CountDeadLetterRecordsByTarget()
	if err != nil {
		t.Fatalf("CountDeadLetterRecordsByTarget: %v", err)
	}

	if counts.Total != 2 {
		t.Errorf("total: want 2, got %d", counts.Total)
	}
	if counts.Unattributed != 1 {
		t.Errorf("unattributed: want 1 (the no-target record), got %d", counts.Unattributed)
	}
	if got := counts.ForParent("parent-one"); got != 1 {
		t.Errorf("parent-one: want 1, got %d", got)
	}

	// The heart of the finding: a conductor that is not the target must see
	// nothing of its own, no matter how many records are parked.
	for _, stranger := range []string{
		"buildbrain", "joiva", "mnemo", "family-brain", "stayplace", "",
	} {
		if got := counts.ForParent(stranger); got != 0 {
			t.Errorf("%q is not the target and must count 0, got %d", stranger, got)
		}
	}

	// The fleet-wide figure the old single count reported is still available,
	// for the surfaces that legitimately want it.
	if total, err := CountDeadLetterRecords(); err != nil || total != counts.Total {
		t.Errorf("fleet-wide count: want %d/nil, got %d/%v", counts.Total, total, err)
	}
}

// A record nobody can parse is a record nobody can be assigned — it must count
// as unattributed rather than vanish, which is what keeps a truncated legacy
// append from making a non-empty ledger look clean (#1877).
func TestUnparseableDeadLetterCountsAsUnattributed(t *testing.T) {
	parkDeadLetterForTest(t, "bbbb2222-1700000000", `{"child_session_id":"trunc`)

	counts, err := CountDeadLetterRecordsByTarget()
	if err != nil {
		t.Fatalf("CountDeadLetterRecordsByTarget: %v", err)
	}
	if counts.Total != 1 || counts.Unattributed != 1 {
		t.Fatalf("want total 1 / unattributed 1, got total %d / unattributed %d",
			counts.Total, counts.Unattributed)
	}
	if len(counts.ForTarget) != 0 {
		t.Errorf("an unreadable record must not be attributed to anyone, got %v", counts.ForTarget)
	}
}

// An absent store is a clean state, not an error: a fleet that has never
// dead-lettered anything has no directory.
func TestDeadLetterCountsWithNoStore(t *testing.T) {
	_ = os.RemoveAll(DeadLetterDir())
	counts, err := CountDeadLetterRecordsByTarget()
	if err != nil {
		t.Fatalf("an absent dead-letter dir must not be an error: %v", err)
	}
	if counts.Total != 0 || counts.Unattributed != 0 || len(counts.ForTarget) != 0 {
		t.Fatalf("want empty counts, got %+v", counts)
	}
	if counts.ForParent("anyone") != 0 {
		t.Error("ForParent on empty counts must be 0")
	}
}

// ForParent must be safe on a zero value — the counts struct travels through
// error paths where the map was never built.
func TestForParentOnZeroValue(t *testing.T) {
	var counts DeadLetterCounts
	if counts.ForParent("someone") != 0 {
		t.Error("zero-value ForParent must be 0, not a panic")
	}
}
