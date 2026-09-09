package send

// ---------------------------------------------------------------------------
// Ghost text is a model suggestion, not a user instruction — and harvesting it
// would turn one into the other.
//
// Observed 2026-09-09 across 17 idle sessions: EVERY one carried text in its
// composer, and NONE of it was typed. It was Claude Code's tab-to-accept
// suggestion, rendered dim (ESC[2m), absent from the session transcript, and
// made to vanish by typing a single space. The content deceives completely,
// because each suggestion is the perfectly fitting answer to that session's
// last question:
//
//	sp-nuki            "schick swantje den neuen code per whatsapp"
//	mnm-enchrichment   "ich habe erlaubt geklickt, publiziere das konzept"
//	mnm-orchestration  "ja, bereite die L4-Nachforderung für Niklas auf"
//
// Delivered as pending instructions those would have been a real WhatsApp
// message to a real person, two publications claiming an authorisation nobody
// gave, and a demand for money — all invented by the model and executed as the
// operator's will.
//
// agent-deck already refuses to harvest them: ComposerDraft gates on
// ComposerBodyIsSuggestion before anything is saved, cleared or restored, so a
// suggestion is never carried anywhere. These tests hold that line against the
// three real strings, in the rendering Claude Code actually emits, because the
// guard is invisible in normal operation and the failure it prevents is not
// recoverable.
//
// The third test is the reason the whole design hinges on ONE calling
// convention: stripped of ANSI, ghost text and real input are the same bytes.
// Every caller must pass the `capture-pane -e` bytes and let the helper strip
// for text extraction — never the other way round.
// ---------------------------------------------------------------------------

import "testing"

// Mircos drei echte Ghost-Strings vom 09.09., in der Form, in der Claude Code
// sie rendert: Marker in eigener Farbe, dann ESC[2m für den Vorschlag.
func TestGhostTextIsNeverHarvested(t *testing.T) {
	for _, body := range []string{
		"schick swantje den neuen code per whatsapp",
		"ich habe erlaubt geklickt, publiziere das konzept",
		"ja, bereite die L4-Nachforderung für Niklas auf",
	} {
		t.Run(body[:12], func(t *testing.T) {
			ghost := pane("\x1b[39m❯ \x1b[2m" + body + "\x1b[0m")
			if !ComposerBodyIsSuggestion(ghost) {
				t.Fatalf("dim body must be a suggestion: %q", body)
			}
			if draft, _ := ComposerDraft(ghost, nil); draft != "" {
				t.Errorf("a suggestion must never be harvested, got %q", draft)
			}
			if ComposerHasDraft(ghost, nil) {
				t.Error("ComposerHasDraft must be false for a suggestion")
			}
		})
	}
}

func TestRealInputIsStillHarvested(t *testing.T) {
	real := renderComposer("schick swantje den neuen code per whatsapp")
	if ComposerBodyIsSuggestion(real) {
		t.Fatal("non-dim body must NOT be a suggestion")
	}
	if !ComposerHasDraft(real, nil) {
		t.Fatal("non-dim body must be seen as a real draft")
	}
}

// Warum jeder Aufrufer die ROHEN Bytes übergeben muss: gestrippt ist Ghost von
// echter Eingabe nicht mehr zu unterscheiden.
func TestStrippedGhostIsIndistinguishable(t *testing.T) {
	if !ComposerHasDraft(pane("❯ ich habe erlaubt geklickt, publiziere das konzept"), nil) {
		t.Fatal("stripped ghost reads as a real draft — which is why every caller " +
			"must pass the -e capture with ANSI intact")
	}
}
