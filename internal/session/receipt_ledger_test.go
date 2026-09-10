package session

import (
	"os"
	"strconv"
	"testing"
	"time"
)

func mitEigenemDatenverzeichnis(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// Der Zweck des Buchs: eine Wiederholung als Wiederholung erkennen.
func TestQuittungsbuch_ErkenntWiederholung(t *testing.T) {
	mitEigenemDatenverzeichnis(t)
	const inst, hash = "inst-1", "a1b2c3d4e5f60718"

	if gelesen, _ := PromptWasRead(inst, hash, time.Hour); gelesen {
		t.Fatal("leeres Buch meldet einen Treffer")
	}
	RecordPromptRead(inst, hash, "p-1")
	gelesen, wann := PromptWasRead(inst, hash, time.Hour)
	if !gelesen {
		t.Fatal("gerade eingetragene Nachricht wird nicht wiedererkannt")
	}
	if time.Since(wann) > time.Minute {
		t.Errorf("Lesezeitpunkt %v ist nicht plausibel", wann)
	}
}

// Fremde Nachrichten und fremde Sitzungen duerfen nie treffen -- sonst
// unterdrueckt --once eine Anweisung, die nie ankam.
func TestQuittungsbuch_TrenntNachrichtUndSitzung(t *testing.T) {
	mitEigenemDatenverzeichnis(t)
	RecordPromptRead("inst-1", "aaaa1111bbbb2222", "p-1")

	if gelesen, _ := PromptWasRead("inst-1", "cccc3333dddd4444", time.Hour); gelesen {
		t.Error("fremder Abdruck trifft")
	}
	if gelesen, _ := PromptWasRead("inst-2", "aaaa1111bbbb2222", time.Hour); gelesen {
		t.Error("Eintrag einer anderen Sitzung trifft")
	}
}

// Das Fenster begrenzt, wie weit zurueck eine Wiederholung als solche gilt.
// Ohne das wuerde eine Anweisung, die vor Wochen einmal gelesen wurde, heute
// stillschweigend unterdrueckt.
func TestQuittungsbuch_AchtetDasFenster(t *testing.T) {
	mitEigenemDatenverzeichnis(t)
	const inst, hash = "inst-1", "a1b2c3d4e5f60718"
	RecordPromptRead(inst, hash, "p-1")

	// Eintrag kuenstlich altern lassen.
	pfad := ReceiptLedgerPath(inst)
	alt := time.Now().Add(-48 * time.Hour).Unix()
	os.WriteFile(pfad, []byte(`{"hash":"`+hash+`","ts":`+strconv.FormatInt(alt, 10)+"}\n"), 0o600)

	if gelesen, _ := PromptWasRead(inst, hash, time.Hour); gelesen {
		t.Error("ein 48h alter Eintrag trifft in einem 1h-Fenster")
	}
	if gelesen, _ := PromptWasRead(inst, hash, 0); !gelesen {
		t.Error("ohne Fenster muesste er treffen")
	}
}

// Das Buch darf nicht unbegrenzt wachsen, aber die JUENGSTEN Eintraege muessen
// bleiben -- die alten sind es, die niemand mehr braucht.
func TestQuittungsbuch_BleibtBegrenztUndBehaeltDasNeueste(t *testing.T) {
	mitEigenemDatenverzeichnis(t)
	const inst = "inst-1"
	for i := 0; i < receiptLedgerMax+50; i++ {
		RecordPromptRead(inst, "hash"+strconv.Itoa(i), "")
	}
	data, err := os.ReadFile(ReceiptLedgerPath(inst))
	if err != nil {
		t.Fatalf("Buch nicht lesbar: %v", err)
	}
	zeilen := 0
	for _, b := range data {
		if b == '\n' {
			zeilen++
		}
	}
	if zeilen > receiptLedgerMax {
		t.Errorf("%d Zeilen, Grenze ist %d", zeilen, receiptLedgerMax)
	}
	if gelesen, _ := PromptWasRead(inst, "hash"+strconv.Itoa(receiptLedgerMax+49), 0); !gelesen {
		t.Error("der juengste Eintrag wurde weggeschnitten")
	}
}

// Ein leerer Abdruck darf nichts eintragen und auf nichts passen: sonst
// wuerde eine Nachricht ohne verwertbaren Inhalt jede andere unterdruecken.
func TestQuittungsbuch_IgnoriertLeereAbdruecke(t *testing.T) {
	mitEigenemDatenverzeichnis(t)
	RecordPromptRead("inst-1", "", "p-1")
	if gelesen, _ := PromptWasRead("inst-1", "", time.Hour); gelesen {
		t.Error("leerer Abdruck trifft")
	}
	if _, err := os.Stat(ReceiptLedgerPath("inst-1")); err == nil {
		t.Error("fuer einen leeren Abdruck wurde eine Datei angelegt")
	}
}
