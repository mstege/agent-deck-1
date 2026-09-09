package main

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// `session send` had no wall-clock bound of its own.
//
// Every phase carries a budget — the defer hold (--defer-timeout, 30m),
// readiness and completion (--timeout, 10m), the submit verification loop
// (50 × 300ms) — and each is enforced only from inside that phase. A phase that
// wedges before its own deadline check, or a caller that reaches one of the
// paths between phases, is unbounded. Observed 2026-09-09: nine `session send`
// processes alive at once, the oldest for 58 minutes, none of them making
// progress and none of them going to exit on their own. They are cheap
// individually and ruinous in aggregate — a dispatcher that fires twenty
// messages accumulates twenty of them, each holding an open DB handle and
// polling on a cadence, which is load the fleet then has to absorb.
//
// The watchdog is the outermost bound and deliberately knows nothing about the
// phases it supervises: it is armed once, from the parsed flags, and it fires
// whatever the reason. A per-phase timeout can only bound the failure modes its
// author anticipated; this bounds the ones nobody did.
// ---------------------------------------------------------------------------

// sendWatchdogSlack is how much longer than the sum of a send's own declared
// budgets the watchdog waits before it fires. It exists so the watchdog never
// pre-empts a phase that is still legitimately inside its own deadline — the
// phases must report their own, better-diagnosed errors first. Generous on
// purpose: this is the backstop for a wedge, not a second timeout.
const sendWatchdogSlack = 2 * time.Minute

// sendWatchdog terminates the process when a send outlives every budget it was
// given. phase is updated as the send proceeds so the message can name where it
// wedged; the operator otherwise gets a bare timeout against a command whose
// argv says nothing about how far it got.
type sendWatchdog struct {
	current atomic.Value // string
}

func (w *sendWatchdog) phase(name string) {
	if w == nil {
		return
	}
	w.current.Store(name)
}

func (w *sendWatchdog) phaseName() string {
	if w == nil {
		return "unknown"
	}
	if v, ok := w.current.Load().(string); ok && v != "" {
		return v
	}
	return "unknown"
}

// sendBudget returns the wall-clock bound for one `session send`: the sum of
// the budgets its own flags declare, plus slack.
//
// Summed rather than maxed because the phases run in sequence — a send may hold
// for the full defer timeout and only then start waiting for readiness — so a
// bound of max(...) would fire during legitimate work.
func sendBudget(deferIfBusy bool, deferTimeout, timeout time.Duration, wait bool) time.Duration {
	total := sendWatchdogSlack
	if deferIfBusy && deferTimeout > 0 {
		total += deferTimeout
	}
	if timeout > 0 {
		// Once for the readiness phase, and again for the completion wait that
		// --wait/--stream run against the same flag.
		total += timeout
		if wait {
			total += timeout
		}
	}
	return total
}

// armSendWatchdog starts the backstop. The returned watchdog is used to label
// the current phase; the timer runs for the life of the process.
//
// It exits rather than cancelling cooperatively, and that is the point: a
// cooperative cancel has to be received, and the states this exists for are the
// ones where nothing is receiving. Exit code 1 matches every other delivery
// failure, and the message says explicitly that delivery is unknown — a wedged
// send may well have typed the message before it hung, so the operator must
// look before resending.
func armSendWatchdog(budget time.Duration, out *CLIOutput) *sendWatchdog {
	w := &sendWatchdog{}
	w.phase("startup")
	if budget <= 0 || out == nil {
		return w
	}
	time.AfterFunc(budget, func() {
		phase := w.phaseName()
		out.ErrorWithData(fmt.Sprintf(
			"session send exceeded its overall budget of %s while in phase %q and was terminated. "+
				"Delivery is UNKNOWN — check the target before resending, a blind resend duplicates the message",
			budget, phase),
			ErrCodeDeliveryFailed,
			map[string]interface{}{
				"delivery":  deliveryUnobserved,
				"submitted": false,
				"phase":     phase,
			})
		os.Exit(1)
	})
	return w
}
