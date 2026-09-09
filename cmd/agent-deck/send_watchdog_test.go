package main

import (
	"testing"
	"time"
)

// The budget must cover every phase a send can legitimately spend time in, or
// the backstop becomes a second, shorter timeout that pre-empts real work.
func TestSendBudgetCoversTheDeclaredPhases(t *testing.T) {
	tests := []struct {
		name         string
		deferIfBusy  bool
		deferTimeout time.Duration
		timeout      time.Duration
		wait         bool
		want         time.Duration
	}{
		{
			name:    "plain send: readiness plus slack",
			timeout: 10 * time.Minute,
			want:    10*time.Minute + sendWatchdogSlack,
		},
		{
			name:         "defer holds first and readiness runs after it, so the two add up",
			deferIfBusy:  true,
			deferTimeout: 30 * time.Minute,
			timeout:      10 * time.Minute,
			want:         40*time.Minute + sendWatchdogSlack,
		},
		{
			name:         "--defer-timeout without --defer-if-busy buys no time",
			deferTimeout: 30 * time.Minute,
			timeout:      10 * time.Minute,
			want:         10*time.Minute + sendWatchdogSlack,
		},
		{
			name:    "--wait spends the same timeout a second time, waiting for the reply",
			timeout: 10 * time.Minute,
			wait:    true,
			want:    20*time.Minute + sendWatchdogSlack,
		},
		{
			name: "no budgets at all still leaves the slack as the bound",
			want: sendWatchdogSlack,
		},
		{
			name:    "a negative timeout is not subtracted from the bound",
			timeout: -time.Minute,
			want:    sendWatchdogSlack,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sendBudget(tc.deferIfBusy, tc.deferTimeout, tc.timeout, tc.wait)
			if got != tc.want {
				t.Fatalf("sendBudget = %s, want %s", got, tc.want)
			}
		})
	}
}

// The message has to name the phase, because the argv of a wedged send says
// nothing about how far it got — which is exactly the position the nine hanging
// processes of 2026-09-09 left their operator in.
func TestWatchdogReportsTheCurrentPhase(t *testing.T) {
	w := &sendWatchdog{}
	if got := w.phaseName(); got != "unknown" {
		t.Errorf("un-labelled watchdog: want %q, got %q", "unknown", got)
	}
	w.phase("defer-if-busy")
	if got := w.phaseName(); got != "defer-if-busy" {
		t.Errorf("phase: want %q, got %q", "defer-if-busy", got)
	}
	w.phase("deliver")
	if got := w.phaseName(); got != "deliver" {
		t.Errorf("phase: want %q, got %q", "deliver", got)
	}
}

// A nil watchdog must be usable: the labelling calls are sprinkled through the
// send path and must never be the thing that panics it.
func TestNilWatchdogIsInert(t *testing.T) {
	var w *sendWatchdog
	w.phase("deliver")
	if got := w.phaseName(); got != "unknown" {
		t.Fatalf("nil watchdog phase: want %q, got %q", "unknown", got)
	}
}

// A non-positive budget must not arm a timer that fires immediately.
func TestArmSendWatchdogWithNoBudgetDoesNotFire(t *testing.T) {
	w := armSendWatchdog(0, NewCLIOutput(false, false))
	if w == nil {
		t.Fatal("armSendWatchdog must always return a usable watchdog")
	}
	time.Sleep(20 * time.Millisecond)
	if got := w.phaseName(); got != "startup" {
		t.Fatalf("phase: want %q, got %q", "startup", got)
	}
}
