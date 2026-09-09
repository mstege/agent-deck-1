package session

import "time"

// PromptReceipt is a sample of an instance's hook status file taken for one
// purpose: deciding whether the inner agent has durably acknowledged a prompt
// that agent-deck just typed into its pane.
//
// Why a receipt at all. Every other signal `session send` has is read off the
// pane — a status heuristic derived from pane diffs, a composer glyph, the
// message body echoed back. All three go blind at exactly the moment they are
// needed most: under machine load `capture-pane` is SIGKILLed on its 3s
// deadline, and the verification loop then reports "no evidence of delivery"
// (issue #876) without having looked at anything. A hook receipt is immune to
// that. Claude's UserPromptSubmit hook writes this file the moment the prompt
// is accepted as a turn — one small local file, no subprocess, no tmux server
// round-trip — so it stays readable on a machine too busy to capture a pane.
//
// It is a one-way signal. A positive receipt proves acceptance; its absence
// proves nothing, because the tool may not run hooks, the message may be queued
// behind a live turn (no UserPromptSubmit until it is taken up), or the file may
// simply not have been written yet. Callers must treat "no receipt" as "keep
// looking", never as "not delivered".
type PromptReceipt struct {
	// present reports that a hook status file was readable at sample time.
	present bool
	// event is the hook event that last wrote the file (e.g.
	// "UserPromptSubmit", "Stop", "SessionStart").
	event string
	// sessionID is the agent session the sample belongs to. A change here
	// means /clear or a restart, not a new turn.
	sessionID string
	// updatedAt is the record's own timestamp. Claude writes it with
	// one-second resolution, which is why it is never used alone.
	updatedAt time.Time
	// sequence is the hook writer's monotonic counter where it emits one; 0
	// when absent.
	sequence uint64
}

// promptSubmitEvent is the hook event Claude writes when it accepts a prompt as
// a turn. It is the only event that constitutes a delivery receipt: Stop,
// SessionStart and PermissionRequest all describe a session that is NOT taking
// up new input.
const promptSubmitEvent = "UserPromptSubmit"

// SamplePromptReceipt reads instanceID's hook status file. A missing or
// unreadable file yields a zero sample, which never satisfies AcceptedSince.
func SamplePromptReceipt(instanceID string) PromptReceipt {
	hs := readHookStatusFile(instanceID)
	if hs == nil {
		return PromptReceipt{}
	}
	return PromptReceipt{
		present:   true,
		event:     hs.Event,
		sessionID: hs.SessionID,
		updatedAt: hs.UpdatedAt,
		sequence:  hs.Sequence,
	}
}

// AcceptedSince reports whether r is evidence that the agent accepted a prompt
// after the `before` sample was taken.
//
// It requires two things, and the second is what keeps it honest. First, the
// current record must be a prompt-submit event — a Stop or SessionStart record
// says the opposite. Second, the record must be demonstrably NEWER than the
// pre-send sample: a UserPromptSubmit that was already sitting in the file
// before we typed anything belongs to somebody else's turn, and counting it
// would certify a send that never landed. That is the same "only a transition
// counts, never a snapshot" rule the arrival baseline follows.
//
// Ties resolve to false. Claude's `ts` has one-second resolution, so a prompt
// submitted in the same second as an earlier one is indistinguishable from it
// unless the writer also emits a sequence. Missing the receipt costs a fallback
// to the pane signals; inventing one would certify a lost message.
func (r PromptReceipt) AcceptedSince(before PromptReceipt) bool {
	if !r.present || r.event != promptSubmitEvent {
		return false
	}
	if !before.present {
		// Nothing to compare against: the file appeared during our send. A
		// prompt-submit record that did not exist before we typed is ours.
		return true
	}
	if r.sessionID != before.sessionID {
		// A different agent session wrote this. /clear and restart land here;
		// so does a resumed session picking up a new id. Either way the record
		// cannot be a leftover of the pre-send state.
		return true
	}
	if r.sequence != 0 || before.sequence != 0 {
		return r.sequence > before.sequence
	}
	return r.updatedAt.After(before.updatedAt)
}

// SessionReplacedSince reports whether the agent session behind this instance
// was replaced after the `before` sample — a different agent session id in the
// hook record.
//
// It exists for the one class of input whose successful execution DESTROYS
// every pane-based proof of its own delivery: a session-resetting slash
// command. `/clear` produces no "active" transition, leaves no composer
// remnant, and erases its own line from the pane, so the three signals the
// verification loop looks for are all absent precisely BECAUSE it worked. The
// loop then reported a completed `/clear` as "send dropped silently … before
// retrying" (observed 2026-09-09 against session `vora`, and reproduced: the
// pane showed `❯ /clear`, the session was empty, costs were back to $0, and
// the command exited non-zero recommending a resend).
//
// A new agent session id is the evidence that survives, because nothing but a
// session reset produces one. It is NOT delivery evidence in general, though:
// for an ordinary message a session that changed identity mid-send was
// restarted under us, and the message is more likely lost than delivered.
// Callers must therefore pair this with "was the input a command that resets
// the session" — see resetsAgentSession in the send path — rather than
// treating an id change as a receipt on its own.
func (r PromptReceipt) SessionReplacedSince(before PromptReceipt) bool {
	if !r.present || r.sessionID == "" {
		return false
	}
	if !before.present || before.sessionID == "" {
		// No pre-send identity to compare against: an id appearing out of
		// nothing is not evidence that OUR input replaced anything.
		return false
	}
	return r.sessionID != before.sessionID
}
