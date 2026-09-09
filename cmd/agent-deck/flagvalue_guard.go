package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Go's flag package takes the argument after a string flag as its value without
// looking at it. So `add --worktree -b .` does not report a missing value: it
// silently names the worktree "-b", and `--worktree --model` names it
// "--model". Both then run all the way through — the branch prefix from
// [worktree].branch_prefix is applied on top, so a mistyped command line
// produced real branches called `feature/-b`, `feature/--branch` and
// `feature/--model` on 2026-09-09, one after another.
//
// The failure is quiet in both directions: the flag that got swallowed is
// simply absent, so `--worktree x -b` creates the worktree without the new
// branch it asked for, and nothing says so.
//
// The guard is deliberately a denylist of identity-shaped flags rather than a
// blanket rule. Some flags legitimately carry values that begin with a dash —
// `--extra-arg --verbose` passes a CLI token through to the inner agent, and
// `--cmd` carries a whole command line. For those, a leading dash is the point.
// For a branch, a title, a group or a model id it never is.
// ---------------------------------------------------------------------------

// identityFlagNames are the flags whose value names a thing — a branch, a
// title, a group, a model, a parent session, a worktree location. None of those
// can begin with a dash: git refuses such a ref, and the others are looked up
// by name.
var identityFlagNames = map[string]bool{
	"w": true, "worktree": true,
	"t": true, "title": true,
	"g": true, "group": true,
	"p": true, "parent": true,
	"model":    true,
	"location": true,
	"account":  true,
}

// rejectSwallowedFlagValues reports guarded flags whose value was taken from
// the next argument and that argument is itself a flag.
//
// It reads the RAW argv rather than the parsed values, because only the raw
// form distinguishes the mistake from the intent: `--title -x` swallowed a
// flag, `--title=-x` deliberately set a value that starts with a dash. After
// parsing the two are the same string, so a guard placed there would have no
// way to let the deliberate form through — and a guard with no escape hatch
// eventually gets removed rather than worked around.
func rejectSwallowedFlagValues(fs *flag.FlagSet, args []string) error {
	boolFlags := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			boolFlags[f.Name] = true
		}
	})

	var offenders []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			// Everything after the end-of-flags marker is positional.
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") {
			// The explicit form. Whatever follows the "=" is the value the
			// caller meant, dashes and all.
			continue
		}
		if boolFlags[name] || !identityFlagNames[name] || i+1 >= len(args) {
			continue
		}
		next := args[i+1]
		i++ // consumed as this flag's value, correctly or not
		if !strings.HasPrefix(next, "-") || next == "-" {
			continue
		}
		offenders = append(offenders, fmt.Sprintf("%s %s", arg, next))
	}
	if len(offenders) == 0 {
		return nil
	}
	sort.Strings(offenders)
	return fmt.Errorf(
		"%s: the value looks like another flag, so it was almost certainly swallowed — "+
			"`--flag1 --flag2` gives --flag1 the text \"--flag2\" and drops --flag2 entirely. "+
			"Supply the missing value, or write --flag=-value if the dash is intended",
		strings.Join(offenders, ", "))
}
