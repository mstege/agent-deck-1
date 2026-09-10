package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// `session orphans` — the inventory the orphan WARN promised and nobody had.
//
// A session with no ParentSessionID reaches nobody: the transition notifier is
// parent-keyed, so its running→waiting edge resolves to deadLetterReasonOrphan
// and is terminally dropped (internal/session/inbox_outbox.go). The only trace
// is one WARN line per child id in notifier-orphans.log — and that line is an
// EDGE, deduped in memory for the notify-daemon's lifetime. Measured on this
// fleet 2026-09-10: the daemon had been up 25h, so every real orphan's single
// warning had already been spent; the log held 5 lines, all of them from
// throwaway probes started that morning. Meanwhile 270 transition events from
// 15 sessions landed in the notifier log that day, and not one came from any of
// the 111 parentless sessions.
//
// So the state was fully known to the code, one warning deep, and invisible to
// the operator. This command answers it as an INVENTORY instead of an edge, and
// the WARN in transition_notifier.go now names it.
//
// Three deliberate decisions:
//
//  1. It does NOT refresh status. RefreshInstancesForCLIStatus is cache-backed
//     and would be affordable, but a fresh status would make this read like a
//     state report ("is this session stuck?"), which it is not — that question
//     belongs to `fleet status` and to the pane-level filters. This answers a
//     STRUCTURE question: who is responsible for whom. Stored status is shown
//     as context and labelled as such.
//
//  2. It never guesses a parent. A suggestion is emitted only when a conductor
//     named conductor-<group> actually exists; otherwise the row says so and
//     the group is listed under "groups with no matching conductor". The
//     Vitalli group is exactly why: its conductor is called conductor-joiva, so
//     any name-similarity heuristic would have invented an owner for 18
//     sessions.
//
//  3. It exits 0 even with findings. 101 unattributed sessions is this fleet's
//     steady state, and a command that is permanently red is a command whose
//     one real alarm gets ignored (the lesson from the dead-letter drain that
//     failed five unrelated conductors).
const (
	// orphanKindConductor: parentless BY DESIGN — a conductor is a root. The
	// notifier drops its own transitions via deadLetterReasonSelfConductor,
	// which is intentional, not a gap.
	orphanKindConductor = "conductor"
	// orphanKindRoot: parentless and HAS children. Functionally a root even
	// without the conductor flag (Command itself is the case that matters:
	// 17 children, no flag, title not conductor-*, so the notifier files its
	// own edges under "orphan"). Nothing is lost — there is no one above it —
	// but the label was wrong, and a root is not an orphan.
	orphanKindRoot = "root_with_children"
	// orphanKindUnattributed: parentless, childless. These are the ones that
	// report to nobody.
	orphanKindUnattributed = "unattributed"
)

// orphanRow is one parentless session plus the classification that says whether
// that is a problem.
type orphanRow struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Group    string `json:"group"`
	Status   string `json:"status"`
	Kind     string `json:"kind"`
	Children int    `json:"children"`
	// LastActivity is the honest "is this a corpse?" signal, and the reason no
	// tmux probe is needed: an abandoned session shows its age here.
	LastActivity string `json:"last_activity,omitempty"`
	// SuggestedParentID/Title are set only on an exact conductor-<group> match.
	SuggestedParentID    string `json:"suggested_parent_id,omitempty"`
	SuggestedParentTitle string `json:"suggested_parent_title,omitempty"`
	// Note explains an ABSENT suggestion. Never a guess.
	Note string `json:"note,omitempty"`
}

// groupLeaf reduces a stored group path ("projects/devops") to its last
// segment, which is what a conductor is named after.
func groupLeaf(groupPath string) string {
	trimmed := strings.Trim(strings.TrimSpace(groupPath), "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	return parts[len(parts)-1]
}

// conductorsByGroupLeaf indexes conductor sessions by the group name their
// title claims — conductor-mnemo answers for group "mnemo".
//
// Membership is title-OR-flag on purpose: isConductorSessionTitle (the notifier's
// own predicate) keys on the title prefix, while `add --conductor` sets the flag.
// A session that is one but not the other still belongs in this index, or the
// suggestion would disagree with the notifier about who is a root.
func conductorsByGroupLeaf(instances []*session.Instance) map[string]*session.Instance {
	index := map[string]*session.Instance{}
	for _, inst := range instances {
		if inst == nil {
			continue
		}
		lower := strings.ToLower(strings.TrimSpace(inst.Title))
		if !inst.IsConductor && !strings.HasPrefix(lower, "conductor-") {
			continue
		}
		claimed := strings.TrimPrefix(lower, "conductor-")
		if claimed == "" || claimed == lower {
			// Flagged but not conductor-named: it answers for no group by name.
			continue
		}
		// First writer wins; a duplicate conductor name is a separate problem
		// and must not silently change who gets suggested.
		if _, taken := index[claimed]; !taken {
			index[claimed] = inst
		}
	}
	return index
}

// classifyOrphans returns one row per parentless session, sorted by kind
// (unattributed first — that is the actionable class) then title. Pure: no
// storage, no tmux, no clock beyond the instances handed in.
func classifyOrphans(instances []*session.Instance) []orphanRow {
	childCount := map[string]int{}
	for _, inst := range instances {
		if inst == nil {
			continue
		}
		if parent := strings.TrimSpace(inst.ParentSessionID); parent != "" {
			childCount[parent]++
		}
	}
	conductors := conductorsByGroupLeaf(instances)

	rows := make([]orphanRow, 0, len(instances))
	for _, inst := range instances {
		if inst == nil || strings.TrimSpace(inst.ParentSessionID) != "" {
			continue
		}
		row := orphanRow{
			ID:       inst.ID,
			Title:    inst.Title,
			Group:    inst.GroupPath,
			Status:   StatusString(inst.Status),
			Children: childCount[inst.ID],
		}
		if la := inst.LastActivityAt(); !la.IsZero() {
			row.LastActivity = la.UTC().Format(time.RFC3339)
		}
		switch {
		case inst.IsConductor || strings.HasPrefix(strings.ToLower(strings.TrimSpace(inst.Title)), "conductor-"):
			row.Kind = orphanKindConductor
		case row.Children > 0:
			row.Kind = orphanKindRoot
		default:
			row.Kind = orphanKindUnattributed
			leaf := groupLeaf(inst.GroupPath)
			if leaf == "" {
				row.Note = "session has no group, so no conductor can be derived"
				break
			}
			conductor, ok := conductors[strings.ToLower(leaf)]
			if !ok {
				row.Note = fmt.Sprintf("no session named conductor-%s", strings.ToLower(leaf))
				break
			}
			row.SuggestedParentID = conductor.ID
			row.SuggestedParentTitle = conductor.Title
		}
		rows = append(rows, row)
	}

	kindRank := map[string]int{orphanKindUnattributed: 0, orphanKindRoot: 1, orphanKindConductor: 2}
	sort.SliceStable(rows, func(i, j int) bool {
		if kindRank[rows[i].Kind] != kindRank[rows[j].Kind] {
			return kindRank[rows[i].Kind] < kindRank[rows[j].Kind]
		}
		return strings.ToLower(rows[i].Title) < strings.ToLower(rows[j].Title)
	})
	return rows
}

// unmatchedGroupTally counts unattributed rows per group that got no
// suggestion. This is where the Vitalli/conductor-joiva mismatch becomes a
// number instead of 18 separate "no conductor" notes.
func unmatchedGroupTally(rows []orphanRow) []struct {
	Group string `json:"group"`
	Count int    `json:"count"`
} {
	counts := map[string]int{}
	for _, row := range rows {
		if row.Kind != orphanKindUnattributed || row.SuggestedParentID != "" {
			continue
		}
		group := groupLeaf(row.Group)
		if group == "" {
			group = "(no group)"
		}
		counts[group]++
	}
	tally := make([]struct {
		Group string `json:"group"`
		Count int    `json:"count"`
	}, 0, len(counts))
	for group, count := range counts {
		tally = append(tally, struct {
			Group string `json:"group"`
			Count int    `json:"count"`
		}{Group: group, Count: count})
	}
	sort.Slice(tally, func(i, j int) bool {
		if tally[i].Count != tally[j].Count {
			return tally[i].Count > tally[j].Count
		}
		return tally[i].Group < tally[j].Group
	})
	return tally
}

// handleSessionOrphans implements `session orphans`.
func handleSessionOrphans(profile string, args []string) {
	fs := flag.NewFlagSet("session orphans", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output as JSON")
	quiet := fs.Bool("quiet", false, "Minimal output (counts only)")
	quietShort := fs.Bool("q", false, "Minimal output (short)")
	unattributedOnly := fs.Bool("unattributed", false, "Only sessions that are parentless AND childless (the actionable class)")
	groupFilter := fs.String("group", "", "Restrict to one group (matches the group's last path segment, case-insensitive)")
	fs.Usage = func() {
		fmt.Println("Usage: agent-deck session orphans [options]")
		fmt.Println()
		fmt.Println("List sessions with no parent, classified by whether that is a problem:")
		fmt.Println("  conductor           parentless by design (a conductor is a root)")
		fmt.Println("  root_with_children  parentless but has children — a root, not an orphan")
		fmt.Println("  unattributed        parentless and childless — reports to nobody")
		fmt.Println()
		fmt.Println("An unattributed session's transition events resolve to no target and are")
		fmt.Println("dropped, so it never signals a conductor that it finished or got stuck.")
		fmt.Println("Link one with: agent-deck session set-parent <id> <parent>")
		fmt.Println()
		fmt.Println("Read-only. Status is the STORED value, not a fresh probe — this answers")
		fmt.Println("who is responsible for whom, not whether a session is stuck. For liveness")
		fmt.Println("use `agent-deck fleet status`.")
		fmt.Println()
		fmt.Println("Options:")
		fs.PrintDefaults()
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  agent-deck session orphans")
		fmt.Println("  agent-deck session orphans --unattributed --group mnemo")
		fmt.Println("  agent-deck session orphans --json")
	}
	if err := fs.Parse(normalizeArgs(fs, args)); err != nil {
		os.Exit(1)
	}
	quietMode := *quiet || *quietShort
	out := NewCLIOutput(*jsonOutput, quietMode)

	_, instances, _, err := loadSessionData(profile)
	if err != nil {
		out.Error(err.Error(), ErrCodeNotFound)
		os.Exit(1)
	}

	rows := classifyOrphans(instances)
	// The tally is computed BEFORE filtering: a --group view must not make the
	// fleet-wide mismatch look smaller than it is.
	tally := unmatchedGroupTally(rows)

	counts := map[string]int{}
	for _, row := range rows {
		counts[row.Kind]++
	}

	filtered := rows
	if *unattributedOnly {
		kept := make([]orphanRow, 0, len(filtered))
		for _, row := range filtered {
			if row.Kind == orphanKindUnattributed {
				kept = append(kept, row)
			}
		}
		filtered = kept
	}
	if leaf := strings.ToLower(groupLeaf(*groupFilter)); leaf != "" {
		kept := make([]orphanRow, 0, len(filtered))
		for _, row := range filtered {
			if strings.ToLower(groupLeaf(row.Group)) == leaf {
				kept = append(kept, row)
			}
		}
		filtered = kept
	}

	var human strings.Builder
	fmt.Fprintf(&human, "Parentless sessions: %d of %d (%d unattributed, %d roots with children, %d conductors)\n",
		len(rows), len(instances), counts[orphanKindUnattributed], counts[orphanKindRoot], counts[orphanKindConductor])
	if len(filtered) == 0 {
		human.WriteString("  (none matching)\n")
	}
	lastKind := ""
	for _, row := range filtered {
		if row.Kind != lastKind {
			fmt.Fprintf(&human, "\n%s:\n", row.Kind)
			lastKind = row.Kind
		}
		activity := row.LastActivity
		if activity == "" {
			activity = "-"
		}
		fmt.Fprintf(&human, "  %s  %-26s  %-14s  %-8s  last=%s",
			row.ID, row.Title, groupLeaf(row.Group), row.Status, activity)
		switch {
		case row.SuggestedParentTitle != "":
			fmt.Fprintf(&human, "  → set-parent %s %s", row.ID, row.SuggestedParentTitle)
		case row.Note != "":
			fmt.Fprintf(&human, "  (%s)", row.Note)
		case row.Children > 0:
			fmt.Fprintf(&human, "  children=%d", row.Children)
		}
		human.WriteString("\n")
	}
	if len(tally) > 0 {
		human.WriteString("\ngroups with no matching conductor:\n")
		for _, entry := range tally {
			fmt.Fprintf(&human, "  %-14s  %d session(s)\n", entry.Group, entry.Count)
		}
		human.WriteString("  (a conductor answers for the group its title names: conductor-<group>)\n")
	}

	out.Print(human.String(), map[string]interface{}{
		"total_sessions":   len(instances),
		"parentless":       len(rows),
		"counts":           counts,
		"orphans":          filtered,
		"unmatched_groups": tally,
	})
}
