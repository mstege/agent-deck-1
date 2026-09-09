package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

func TestIssue1877_AutoParentIdentityMustResolve(t *testing.T) {
	t.Setenv("AGENT_DECK_SESSION_ID", "")
	t.Setenv("AGENTDECK_INSTANCE_ID", "stale-parent-1877")
	parent, unresolved := resolveAutoParentInstanceChecked(nil)
	if parent != nil || unresolved != "stale-parent-1877" {
		t.Fatalf("stale injected identity must fail loudly: parent=%v unresolved=%q", parent, unresolved)
	}
}

func TestIssue1877_InboxDrainReportsDeadLettersAndIsNonZero(t *testing.T) {
	cliInboxTestHome(t)
	registerInboxDrainTarget(t, "parent-1877")
	event := session.TransitionNotificationEvent{
		ChildSessionID: "dead-child-1877", Profile: "default",
		FromStatus: "running", ToStatus: "error", Timestamp: time.Now(),
		DeadLetterReason: "unresolvable", Attempts: 5,
	}
	raw := []byte(`{"child_session_id":"dead-child-1877","profile":"default","from_status":"running","to_status":"error","timestamp":"` + event.Timestamp.Format(time.RFC3339Nano) + `","attempts":5,"dead_letter_reason":"unresolvable"}` + "\n")
	if err := os.MkdirAll(session.DeadLetterDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.DeadLetterPathFor(event.ChildSessionID), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	// The #1877 guarantee — an undelivered record must never be reportable as
	// a clean state — now lives on `inbox dead-letters`, the surface built for
	// exactly these records, instead of on every per-parent drain. It used to
	// be enforced on the drain, and that is what made ONE unaddressed record
	// five unrelated conductors' problem on 2026-09-09: each of their heartbeat
	// drains warned and exited 4, each operator dismissed it as "not my area",
	// and each escalated. A guarantee that fires on everybody teaches everybody
	// to ignore it.
	var out bytes.Buffer
	err := runInbox(&out, []string{"dead-letters"})
	var pending *deadLettersPendingError
	if !errors.As(err, &pending) || inboxExitCode(err) == 0 {
		t.Fatalf("dead letters must make `inbox dead-letters` non-clean: err=%v", err)
	}
	if !strings.Contains(out.String(), "1 dead-lettered record") {
		t.Fatalf("dead-letters must report the non-zero count, got %q", out.String())
	}

	// And the record names no target, so no parent's drain may fail on it.
	var drainOut bytes.Buffer
	if drainErr := runInbox(&drainOut, []string{"drain", "parent-1877"}); drainErr != nil {
		t.Fatalf("an unaddressed record must not fail a parent's drain: %v", drainErr)
	}
	if !strings.Contains(drainOut.String(), "belong to no inbox") {
		t.Fatalf("the drain must still say the record exists and is not its work, got %q", drainOut.String())
	}
}

func TestIssue1877_CorruptNonEmptyDeadLetterIsNotReportedClean(t *testing.T) {
	cliInboxTestHome(t)
	registerInboxDrainTarget(t, "parent-corrupt-1877")
	if err := os.MkdirAll(session.DeadLetterDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.DeadLetterPathFor("truncated-1877"), []byte(`{"child_session_id":"truncated`), 0o600); err != nil {
		t.Fatal(err)
	}

	// A record nobody can parse is a record nobody can be assigned, so it
	// counts as unattributed rather than vanishing — that is what stops a
	// truncated legacy append from making a non-empty ledger look clean.
	var out bytes.Buffer
	err := runInbox(&out, []string{"dead-letters"})
	var pending *deadLettersPendingError
	if !errors.As(err, &pending) || pending.count != 1 || inboxExitCode(err) != 4 {
		t.Fatalf("non-empty corrupt dead-letter must be loud: output=%q err=%v", out.String(), err)
	}
	if !strings.Contains(out.String(), "(unattributed)") {
		t.Fatalf("an unreadable record must be listed as unattributed, got %q", out.String())
	}
}

func TestIssue2007_InboxDrainCountsUnownedDiscoveryEntries(t *testing.T) {
	cliInboxTestHome(t)
	registerInboxDrainTarget(t, "parent-2007")
	event := session.TransitionNotificationEvent{
		ChildSessionID: "unowned-child-2007", Profile: "default",
		FromStatus: "running", ToStatus: "error", Timestamp: time.Now(),
	}
	if err := session.WriteInboxEvent(session.UnownedInboxID, event); err != nil {
		t.Fatal(err)
	}

	// The discovery-only _unowned ledger holds records whose owner could not be
	// determined at all — unattributed by construction, and still real operator
	// work. It must be counted, on the surface that owns that question.
	var out bytes.Buffer
	err := runInbox(&out, []string{"dead-letters"})
	var pending *deadLettersPendingError
	if !errors.As(err, &pending) || pending.count != 1 || inboxExitCode(err) != 4 {
		t.Fatalf("unowned discovery must keep dead-letters non-clean: output=%q err=%v", out.String(), err)
	}
	if !strings.Contains(out.String(), "(unattributed)") {
		t.Fatalf("unowned count missing from the dead-letters listing: %q", out.String())
	}

	// It has no target either, so it is nobody's drain.
	var drainOut bytes.Buffer
	if drainErr := runInbox(&drainOut, []string{"drain", "parent-2007"}); drainErr != nil {
		t.Fatalf("an unowned record must not fail a parent's drain: %v", drainErr)
	}
}
