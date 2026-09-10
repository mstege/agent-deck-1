package send

import "testing"

// Der Fingerabdruck muss genau die Unterschiede überleben, die das Terminal
// einführt — und genau die unterscheiden, die eine andere Nachricht ausmachen.
func TestPromptDigest(t *testing.T) {
	basis := PromptDigest("Bitte committe die vier Behebungen und pushe den Branch.")

	gleich := map[string]string{
		"abschliessender Zeilenumbruch": "Bitte committe die vier Behebungen und pushe den Branch.\n",
		"fuehrender Leerraum":           "  Bitte committe die vier Behebungen und pushe den Branch.",
		"NBSP statt Leerzeichen":        "Bitte committe die vier Behebungen und pushe den Branch.",
		"umgebrochene Zeile":            "Bitte committe die vier Behebungen\nund pushe den Branch.",
	}
	for name, variante := range gleich {
		if got := PromptDigest(variante); got != basis {
			t.Errorf("%s ergibt einen anderen Fingerabdruck — ein gelesener Prompt wuerde als ungelesen gemeldet", name)
		}
	}

	verschieden := map[string]string{
		"anderer Text":     "Bitte committe die vier Behebungen und pushe den Branch heute.",
		"Verneinung":       "Bitte committe die vier Behebungen und pushe den Branch NICHT.",
		"andere Nachricht": "ok",
	}
	for name, variante := range verschieden {
		if PromptDigest(variante) == basis {
			t.Errorf("%s ergibt denselben Fingerabdruck — zwei Nachrichten waeren nicht auseinanderzuhalten", name)
		}
	}

	// Leer heisst "kein Fingerabdruck", nicht "Fingerabdruck des Leeren":
	// sonst wuerde eine leere Nachricht auf jeden leeren Beleg passen.
	if PromptDigest("   \n\t ") != "" {
		t.Error("Leerraum ergibt einen Fingerabdruck")
	}
}
