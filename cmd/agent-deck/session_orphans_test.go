package main

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// The fleet shape these tests encode was measured on the live registry
// 2026-09-10: 151 sessions, 111 of them parentless — 5 conductors, 5 roots that
// have children, 101 unattributed. Classifying all 111 as "orphans" was the
// mislabel worth a test: two of the three classes are correct by construction.

func inst(id, title, group, parent string, isConductor bool) *session.Instance {
	return &session.Instance{
		ID:              id,
		Title:           title,
		GroupPath:       group,
		ParentSessionID: parent,
		IsConductor:     isConductor,
	}
}

func rowByTitle(t *testing.T, rows []orphanRow, title string) orphanRow {
	t.Helper()
	for _, row := range rows {
		if row.Title == title {
			return row
		}
	}
	t.Fatalf("no row for %q in %+v", title, rows)
	return orphanRow{}
}

func TestClassifyOrphansSeparatesRootsFromUnattributed(t *testing.T) {
	instances := []*session.Instance{
		inst("c1", "conductor-mnemo", "conductor", "", true),
		inst("cmd", "Command", "active", "", false),
		inst("k1", "mnm-perf", "mnemo", "cmd", false),
		inst("o1", "mnm-ts-1203", "mnemo", "", false),
	}

	rows := classifyOrphans(instances)
	if len(rows) != 3 {
		t.Fatalf("want 3 parentless rows (the child must not appear), got %d: %+v", len(rows), rows)
	}

	if kind := rowByTitle(t, rows, "conductor-mnemo").Kind; kind != orphanKindConductor {
		t.Errorf("a conductor is a root by design, want %q, got %q", orphanKindConductor, kind)
	}
	// Command is the case that motivated the split: 17 children, no conductor
	// flag, title not conductor-*, so the notifier files its own edges under
	// "orphan". Nothing is lost — there is nobody above it.
	command := rowByTitle(t, rows, "Command")
	if command.Kind != orphanKindRoot {
		t.Errorf("a parentless session WITH children is a root, want %q, got %q", orphanKindRoot, command.Kind)
	}
	if command.Children != 1 {
		t.Errorf("want child count 1 for Command, got %d", command.Children)
	}
	if command.SuggestedParentID != "" {
		t.Errorf("a root must get no parent suggestion, got %q", command.SuggestedParentID)
	}

	orphan := rowByTitle(t, rows, "mnm-ts-1203")
	if orphan.Kind != orphanKindUnattributed {
		t.Errorf("parentless and childless is the actionable class, want %q, got %q", orphanKindUnattributed, orphan.Kind)
	}
	if orphan.SuggestedParentID != "c1" || orphan.SuggestedParentTitle != "conductor-mnemo" {
		t.Errorf("group mnemo must resolve to conductor-mnemo, got %q/%q", orphan.SuggestedParentID, orphan.SuggestedParentTitle)
	}
	if orphan.Note != "" {
		t.Errorf("a matched suggestion needs no note, got %q", orphan.Note)
	}
}

// The live mismatch: 18 Vitalli sessions and a conductor called
// conductor-joiva. Any name-similarity heuristic would have invented an owner
// for all 18, and set-parent is not reversible without knowing the old value.
func TestClassifyOrphansNeverGuessesAParent(t *testing.T) {
	instances := []*session.Instance{
		inst("c1", "conductor-joiva", "conductor", "", true),
		inst("o1", "joi-storage", "Vitalli", "", false),
		inst("o2", "oma-spiele", "", "", false),
	}

	rows := classifyOrphans(instances)

	vitalli := rowByTitle(t, rows, "joi-storage")
	if vitalli.SuggestedParentID != "" {
		t.Errorf("group Vitalli has no conductor-vitalli; want no suggestion, got %q", vitalli.SuggestedParentID)
	}
	if !strings.Contains(vitalli.Note, "conductor-vitalli") {
		t.Errorf("the note must name the session that would have answered, got %q", vitalli.Note)
	}

	ungrouped := rowByTitle(t, rows, "oma-spiele")
	if ungrouped.SuggestedParentID != "" || !strings.Contains(ungrouped.Note, "no group") {
		t.Errorf("a session without a group can derive nothing, got %q / note %q", ungrouped.SuggestedParentID, ungrouped.Note)
	}

	tally := unmatchedGroupTally(rows)
	if len(tally) != 2 {
		t.Fatalf("want one tally entry per unmatched group, got %+v", tally)
	}
	if tally[0].Group != "Vitalli" && tally[1].Group != "Vitalli" {
		t.Errorf("the tally must name Vitalli, got %+v", tally)
	}
}

// The notifier decides who is a root by TITLE PREFIX (isConductorSessionTitle),
// while `add --conductor` sets a FLAG. If this index used only one of the two,
// the suggestion would disagree with the notifier about who is a root.
func TestConductorIndexAcceptsTitleOrFlag(t *testing.T) {
	byTitleOnly := inst("t1", "conductor-stayplace", "conductor", "", false)
	flaggedButUnnamed := inst("f1", "Command", "active", "", true)

	index := conductorsByGroupLeaf([]*session.Instance{byTitleOnly, flaggedButUnnamed})

	if got := index["stayplace"]; got != byTitleOnly {
		t.Errorf("a conductor-titled session without the flag must still answer for its group, got %+v", got)
	}
	// A flagged session whose title names no group answers for no group by
	// name — suggesting it for every group would be the guess this avoids.
	for group, inst := range index {
		if inst == flaggedButUnnamed {
			t.Errorf("flagged-but-unnamed session must not claim group %q", group)
		}
	}
}

func TestClassifyOrphansSortsActionableFirst(t *testing.T) {
	instances := []*session.Instance{
		inst("c1", "conductor-mnemo", "conductor", "", true),
		inst("r1", "mnm-orchestration", "mnemo", "", false),
		inst("k1", "worker", "mnemo", "r1", false),
		inst("o2", "zeta", "mnemo", "", false),
		inst("o1", "alpha", "mnemo", "", false),
	}

	rows := classifyOrphans(instances)

	want := []string{"alpha", "zeta", "mnm-orchestration", "conductor-mnemo"}
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, row.Title)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("unattributed first, then roots, then conductors, each alphabetical\nwant %v\ngot  %v", want, got)
	}
}

func TestGroupLeafReducesStoredGroupPaths(t *testing.T) {
	cases := map[string]string{
		"projects/devops": "devops",
		"/mnemo/":         "mnemo",
		"mnemo":           "mnemo",
		"":                "",
		"   ":             "",
	}
	for in, want := range cases {
		if got := groupLeaf(in); got != want {
			t.Errorf("groupLeaf(%q) = %q, want %q", in, got, want)
		}
	}
}
