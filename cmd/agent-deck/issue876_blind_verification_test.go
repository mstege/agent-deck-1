package main

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// Issue #876, load mode: the verification loop must not report a message as
// "dropped silently" when it never managed to observe the target at all.
//
// Observed 2026-09-09 on a fleet of ~20 parallel sessions on a saturated
// machine: `session send` reported "send dropped silently: no evidence of
// delivery after 50 checks (issue #876)" against panes that were sitting at an
// empty, ready composer, and `tmux send-keys` to the very same panes worked. On
// that path `CapturePaneFresh` and `GetStatus` are both bounded at 3s and both
// get SIGKILLed under load, so every branch that could set delivery evidence is
// skipped for the whole budget — and the error then asserts three observations
// ("never went active", "no marker appeared", "body was not visible") that were
// never made.
//
// The distinction is operationally load-bearing, not cosmetic: "dropped" invites
// a resend, and a resend against a target that did receive the message
// duplicates it (#1978) or, on the default path, Ctrl+Cs a live turn (#1979).
// ---------------------------------------------------------------------------

var errProbeKilled = errors.New("failed to capture pane: signal: killed")

func TestBlindVerificationIsNotReportedAsSilentDrop(t *testing.T) {
	mock := &mockSendRetryTarget{
		statuses:   []string{"waiting"},
		statusErrs: []error{errProbeKilled},
		panes:      []string{""},
		paneErrs:   []error{errProbeKilled},
	}
	delivery, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 8, checkDelay: 0, verifyDelivery: true,
	})
	if err == nil {
		t.Fatal("a send that could not be verified must still be an error")
	}
	if delivery == deliveryNoEvidence {
		t.Fatalf("blind verification reported as a silent drop: %v", err)
	}
	if delivery != deliveryUnobserved {
		t.Fatalf("delivery status: want %q, got %q", deliveryUnobserved, delivery)
	}
	// The operator reads the last line and acts on it. It must not send them
	// to resend into a target that may already hold the message.
	if strings.Contains(err.Error(), "dropped") {
		t.Errorf("error must not claim the message was dropped, got: %v", err)
	}
	if !strings.Contains(err.Error(), "before resending") {
		t.Errorf("error must warn against a blind resend, got: %v", err)
	}
}

// The blind path must not reach for the Ctrl+C-and-resend recovery either: a
// destructive branch needs positive evidence, and there is none here. This is
// the #1979 gate seen from the load side.
func TestBlindVerificationSendsNoInterruptAndNoResend(t *testing.T) {
	mock := &mockSendRetryTarget{
		statuses:   []string{"waiting"},
		statusErrs: []error{errProbeKilled},
		panes:      []string{""},
		paneErrs:   []error{errProbeKilled},
	}
	if _, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 30, checkDelay: 0, verifyDelivery: true,
	}); err == nil {
		t.Fatal("expected an unverified-delivery error")
	}
	if got := atomic.LoadInt32(&mock.sendCtrlCCalls); got != 0 {
		t.Errorf("blind loop sent %d Ctrl+C keys; want 0", got)
	}
	if got := atomic.LoadInt32(&mock.sendKeysCalls); got != 1 {
		t.Errorf("blind loop delivered %d times; want exactly 1", got)
	}
}

// A single successful observation is enough to make the loop competent to
// judge: from then on "no evidence" is a real statement about the target and
// must keep its historical #876 classification.
func TestOneSuccessfulObservationKeepsTheNoEvidenceVerdict(t *testing.T) {
	mock := &mockSendRetryTarget{
		statuses:   []string{"waiting", "waiting"},
		statusErrs: []error{errProbeKilled, nil},
		panes:      []string{"", ""},
		paneErrs:   []error{errProbeKilled, errProbeKilled},
	}
	delivery, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 6, checkDelay: 0, verifyDelivery: true,
	})
	if err == nil {
		t.Fatal("expected the #876 no-evidence error")
	}
	if delivery != deliveryNoEvidence {
		t.Fatalf("delivery status: want %q, got %q", deliveryNoEvidence, delivery)
	}
}

// The no-evidence message must report how many checks actually returned an
// observation, so "50 checks" can never again stand for "50 attempts that all
// failed to look".
func TestNoEvidenceErrorReportsObservedCheckCount(t *testing.T) {
	mock := &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{""}}
	_, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 4, checkDelay: 0, verifyDelivery: true,
	})
	if err == nil {
		t.Fatal("expected the #876 no-evidence error")
	}
	if !strings.Contains(err.Error(), "after 4 checks, 4 of which returned an observation") {
		t.Errorf("error should report observed/total checks, got: %v", err)
	}
}

// An unobserved delivery is never reported as submitted in --json.
func TestUnobservedIsNotSubmittedInJSON(t *testing.T) {
	fields := sendDeliveryResult{delivery: deliveryUnobserved}.jsonFields()
	if fields["submitted"] != false {
		t.Errorf("submitted: want false, got %v", fields["submitted"])
	}
	if fields["delivery"] != deliveryUnobserved {
		t.Errorf("delivery: want %q, got %v", deliveryUnobserved, fields["delivery"])
	}
}

// --- the receipt: a positive signal that does not come off the pane ---------

// The load case the receipt exists for: every pane capture and status probe
// fails, and the send is still reported as submitted, because the agent itself
// acknowledged the prompt.
func TestReceiptConfirmsDeliveryWhileEveryPaneProbeFails(t *testing.T) {
	mock := &mockSendRetryTarget{
		statuses:   []string{"waiting"},
		statusErrs: []error{errProbeKilled},
		panes:      []string{""},
		paneErrs:   []error{errProbeKilled},
	}
	delivery, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 8, checkDelay: 0, verifyDelivery: true,
		deliveryReceipt: func() bool { return true },
	})
	if err != nil {
		t.Fatalf("a receipted delivery must succeed, got: %v", err)
	}
	if delivery != deliverySubmitted {
		t.Fatalf("delivery status: want %q, got %q", deliverySubmitted, delivery)
	}
}

// A receipt ends the loop at once: no further Enter nudges into a target that
// has already taken the prompt up, and no route to the Ctrl+C recovery.
func TestReceiptStopsTheLoopWithoutFurtherKeys(t *testing.T) {
	mock := &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{""}}
	if _, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 30, checkDelay: 0, verifyDelivery: true,
		deliveryReceipt: func() bool { return true },
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&mock.sendEnterCalls); got != 0 {
		t.Errorf("sent %d Enter nudges after a receipt; want 0", got)
	}
	if got := atomic.LoadInt32(&mock.sendCtrlCCalls); got != 0 {
		t.Errorf("sent %d Ctrl+C keys after a receipt; want 0", got)
	}
}

// A receipt arriving in the race between the last check and classification is
// still proof, not a failure.
func TestLateReceiptIsHonouredAtClassificationTime(t *testing.T) {
	mock := &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{""}}
	calls := 0
	delivery, err := sendWithRetryTarget(mock, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 3, checkDelay: 0, verifyDelivery: true,
		deliveryReceipt: func() bool {
			calls++
			// Nothing during the budget; true only on the post-budget look.
			return calls > 3
		},
	})
	if err != nil {
		t.Fatalf("a late receipt must still succeed, got: %v", err)
	}
	if delivery != deliverySubmitted {
		t.Fatalf("delivery status: want %q, got %q", deliverySubmitted, delivery)
	}
}

// Absence of a receipt must never become a verdict of its own: with hooks
// silent, the loop's pane-derived classification has to be exactly what it was
// before the receipt existed.
func TestNoReceiptLeavesTheExistingVerdictsUntouched(t *testing.T) {
	never := func() bool { return false }

	blind := &mockSendRetryTarget{
		statuses: []string{"waiting"}, statusErrs: []error{errProbeKilled},
		panes: []string{""}, paneErrs: []error{errProbeKilled},
	}
	if got, _ := sendWithRetryTarget(blind, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 4, checkDelay: 0, verifyDelivery: true, deliveryReceipt: never,
	}); got != deliveryUnobserved {
		t.Errorf("blind target: want %q, got %q", deliveryUnobserved, got)
	}

	silent := &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{""}}
	if got, _ := sendWithRetryTarget(silent, "eine Nachricht an die Session", false, sendRetryOptions{
		maxRetries: 4, checkDelay: 0, verifyDelivery: true, deliveryReceipt: never,
	}); got != deliveryNoEvidence {
		t.Errorf("observed but silent target: want %q, got %q", deliveryNoEvidence, got)
	}

	const msg = "eine Nachricht an die Session"
	held := &mockSendRetryTarget{statuses: []string{"waiting"}, panes: []string{claudeComposer(msg)}}
	if got, _ := sendWithRetryTarget(held, msg, false, sendRetryOptions{
		maxRetries: 4, checkDelay: 0, verifyDelivery: true, deliveryReceipt: never,
	}); got != deliveryTypedNotSubmitted {
		t.Errorf("composer still holding: want %q, got %q", deliveryTypedNotSubmitted, got)
	}
}
