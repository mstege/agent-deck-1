package main

import (
	"fmt"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// pendingPromptNotice renders the "a prompt is in flight" edge for
// `session output`.
//
// THE QUESTION THIS ANSWERS. `session output` returns the last assistant
// response. A caller that has just sent a message reads it to find out whether
// the message arrived — and that is a question the last response cannot answer,
// because between "the agent accepted the prompt" and "the agent's first text
// exists" there is a gap in which the previous turn's report is returned
// unchanged. Unchanged is exactly what a lost message looks like, so the two
// are indistinguishable in precisely the window where a caller is most likely
// to look.
//
// On 2026-09-09 `mnm-orchestration` sent to `sec-backup-restore` at 19:54:43,
// the target accepted the prompt at 19:54:47, and the conductor read
// `session output` at 19:55:07 — 20 seconds into the target's 27-second
// thinking gap. It saw the old report, concluded "the message silently
// failed", committed and pushed the target's uncommitted work itself, and
// wrote the diagnosis into the commit message as established fact. The target
// meanwhile did the same work, found `nothing to commit`, and spent a turn
// reconstructing who had beaten it to it. Every layer worked; `session send`
// had reported `✓ Sent message`, truthfully. The state that settles the
// question — a UserPromptSubmit record newer than the response on screen —
// existed the whole time and was not shown.
//
// ONE-SIDED, LIKE THE RECEIPT IT READS. A notice proves a turn is in flight.
// No notice proves nothing: a tool that runs no hooks writes no record, and a
// message queued behind a live turn produces none until it is taken up. The
// wording therefore never says "nothing is pending" and never contradicts the
// output it accompanies — it only adds an edge the reader could not otherwise
// see.
//
// Returns an empty line and nil fields when there is nothing to claim, so both
// call sites can add it unconditionally.
func pendingPromptNotice(receipt session.PromptReceipt, lastResponseTimestamp string, now time.Time) (string, map[string]interface{}) {
	acceptedAt, pending := receipt.PendingSince(parseResponseTimestamp(lastResponseTimestamp))
	if !pending {
		return "", nil
	}

	fields := map[string]interface{}{
		"prompt_accepted_at": acceptedAt.UTC().Format(time.RFC3339),
		"awaiting_response":  true,
	}

	line := fmt.Sprintf(
		"Pending: this session accepted a prompt at %s%s and has not answered it yet — "+
			"the output below is from BEFORE that prompt. An unchanged report is not evidence "+
			"that a message failed to arrive.",
		acceptedAt.UTC().Format(time.RFC3339),
		humanizeSince(acceptedAt, now),
	)
	return line, fields
}

// humanizeSince renders " (20s ago)" for a timestamp in the past, and the
// empty string when the age is not meaningful — a clock that disagrees with
// the hook writer's would otherwise produce "(-3s ago)", which reads as a bug
// in the very notice that is meant to be trusted.
func humanizeSince(t, now time.Time) string {
	if now.IsZero() {
		return ""
	}
	d := now.Sub(t)
	if d < 0 {
		return ""
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf(" (%ds ago)", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf(" (%dm ago)", int(d.Minutes()))
	default:
		return fmt.Sprintf(" (%dh%dm ago)", int(d.Hours()), int(d.Minutes())%60)
	}
}

// parseResponseTimestamp turns ResponseOutput.Timestamp into a time. The field
// is documented as Claude-only and is empty for several tools, so an
// unparseable value is not an error condition — it yields the zero time, which
// PendingSince reads as "do not compare", not as "the epoch".
func parseResponseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
