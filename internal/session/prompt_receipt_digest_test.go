package session

import (
	"testing"
	"time"
)

func sampleMitAbdruck(event, sid, hash string, ts time.Time, seq uint64) PromptReceipt {
	r := sample(event, sid, ts, seq)
	r.promptHash = hash
	return r
}

// Der inhaltsgebundene Beleg beantwortet die Frage, die der Flankenbeleg offen
// liess: nicht "ein Prompt wurde angenommen", sondern "MEINER wurde gelesen".
func TestAcceptedMessageSince(t *testing.T) {
	vorher := time.Unix(1789000000, 0)
	nachher := time.Unix(1789000030, 0)
	meiner := "a1b2c3d4e5f60718"
	fremder := "ffffffffffffffff"

	tests := []struct {
		name    string
		jetzt   PromptReceipt
		before  PromptReceipt
		digest  string
		treffer bool
	}{
		{
			name:   "mein Abdruck auf einer neueren Annahme-Flanke",
			jetzt:  sampleMitAbdruck(promptSubmitEvent, "s1", meiner, nachher, 2),
			before: sample("Stop", "s1", vorher, 1), digest: meiner, treffer: true,
		},
		{
			name: "fremder Prompt in derselben Sitzung",
			// Der Fall, der den Flankenbeleg blind macht: ein Turn nimmt die
			// eingereihte Nachricht eines anderen Absenders auf, waehrend
			// unsere noch unabgeschickt im Composer steht.
			jetzt:  sampleMitAbdruck(promptSubmitEvent, "s1", fremder, nachher, 2),
			before: sample("Stop", "s1", vorher, 1), digest: meiner, treffer: false,
		},
		{
			name:   "richtiger Abdruck, aber die Flanke ist aelter als unser Send",
			jetzt:  sampleMitAbdruck(promptSubmitEvent, "s1", meiner, vorher, 1),
			before: sampleMitAbdruck(promptSubmitEvent, "s1", meiner, vorher, 1),
			digest: meiner, treffer: false,
		},
		{
			name:   "Stop traegt nie eine Zustellung, auch nicht mit Abdruck",
			jetzt:  sampleMitAbdruck("Stop", "s1", meiner, nachher, 2),
			before: sample("Stop", "s1", vorher, 1), digest: meiner, treffer: false,
		},
		{
			name: "aelterer Hook-Schreiber ohne Abdruck",
			// Muss false ergeben, DARF aber beim Aufrufer keinen Fehlschlag
			// ausloesen: dort faellt es auf den Flankenbeleg zurueck.
			jetzt:  sample(promptSubmitEvent, "s1", nachher, 2),
			before: sample("Stop", "s1", vorher, 1), digest: meiner, treffer: false,
		},
		{
			name:   "leerer Abdruck des Absenders passt auf nichts",
			jetzt:  sampleMitAbdruck(promptSubmitEvent, "s1", meiner, nachher, 2),
			before: sample("Stop", "s1", vorher, 1), digest: "", treffer: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.jetzt.AcceptedMessageSince(tc.before, tc.digest); got != tc.treffer {
				t.Fatalf("AcceptedMessageSince() = %v, erwartet %v", got, tc.treffer)
			}
		})
	}
}

// Die Zusage, die den Umbau ungefaehrlich macht: der stärkere Beleg nimmt dem
// schwächeren nichts weg. Wo der Flankenbeleg vorher trug, traegt er weiter --
// sonst waere aus einer besseren Quittung ein neuer Fehlschlag geworden.
func TestAbdruckSchwaechtDenFlankenbelegNicht(t *testing.T) {
	vorher := time.Unix(1789000000, 0)
	nachher := time.Unix(1789000030, 0)
	ohneAbdruck := sample(promptSubmitEvent, "s1", nachher, 2)
	before := sample("Stop", "s1", vorher, 1)

	if ohneAbdruck.AcceptedMessageSince(before, "a1b2c3d4e5f60718") {
		t.Fatal("ohne Abdruck darf der starke Beleg nicht greifen")
	}
	if !ohneAbdruck.AcceptedSince(before) {
		t.Fatal("der Flankenbeleg traegt nicht mehr — der Umbau haette eine gute Zustellung zum Fehlschlag gemacht")
	}
}
