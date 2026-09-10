package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Das Quittungsbuch: was eine Sitzung tatsächlich als Prompt GELESEN hat.
//
// WOZU. Die Hook-Statusdatei hält immer nur den letzten Datensatz. Damit lässt
// sich beantworten "wurde meine Nachricht gerade eben gelesen", aber nicht
// "wurde sie überhaupt schon einmal gelesen" — und genau das ist die Frage, die
// ein Wiederholungsversuch stellen muss. Ohne sie ist jeder erneute Versuch ein
// Blindflug: er kann eine Anweisung ein zweites Mal ausführen lassen, und
// doppelte Ausführung ist in dieser Flotte die teuerste Fehlerklasse.
//
// WAS DRINSTEHT UND WAS NICHT. Nur der Fingerabdruck, Claudes Prompt-Kennung
// und der Zeitpunkt. Kein Wortlaut: der Eingang einer Sitzung darf keine Kopie
// fremder Anweisungen auf der Platte anlegen, und für die Wiedererkennung
// genügt der Abdruck.
//
// DER MASSSTAB, DEN ES FESTHÄLT. Geschrieben wird ausschließlich auf der
// UserPromptSubmit-Flanke, also dann, wenn der Agent den Prompt als Turn
// angenommen hat. Nicht wenn die Bytes im Pane ankamen, nicht wenn der Composer
// sie hielt. Am 10.09. standen drei unabgeschickte Pastes in laufenden
// Composern hinter Sends, die Erfolg gemeldet hatten — keiner davon hätte hier
// eine Zeile erzeugt, und genau das ist der Sinn.

// receiptLedgerMax bounds the ledger. Wiedererkennung interessiert für die
// Spanne, in der ein Absender einen Wiederholungsversuch erwägt -- Minuten bis
// Stunden, nicht Monate. Eine Datei, die unbegrenzt wächst, wird irgendwann
// entweder langsam oder von Hand gelöscht; beides ist schlechter als eine
// Grenze, die hier steht und begründet ist.
const receiptLedgerMax = 300

type receiptEntry struct {
	Hash     string `json:"hash"`
	PromptID string `json:"prompt_id,omitempty"`
	Ts       int64  `json:"ts"`
}

// ReceiptLedgerPath is the per-instance ledger file.
func ReceiptLedgerPath(instanceID string) string {
	base, err := dataPath("receipts", "receipts")
	if err != nil {
		base = tempAgentDeckPath("receipts")
	}
	return filepath.Join(base, filepath.Base(instanceID)+".jsonl")
}

// RecordPromptRead appends one accepted prompt to instanceID's ledger.
//
// Best effort, immer: dies läuft im Hook-Pfad, der bei jedem Turn feuert. Ein
// Schreibfehler darf einen Turn nie stören -- eine fehlende Zeile kostet die
// Wiedererkennung EINER Nachricht, ein blockierter Hook kostet die Sitzung.
func RecordPromptRead(instanceID, hash, promptID string) {
	hash = strings.TrimSpace(hash)
	if strings.TrimSpace(instanceID) == "" || hash == "" {
		return
	}
	path := ReceiptLedgerPath(instanceID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	line, err := json.Marshal(receiptEntry{Hash: hash, PromptID: promptID, Ts: time.Now().Unix()})
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
	trimReceiptLedger(path)
}

// PromptWasRead reports whether instanceID has already read a prompt with this
// fingerprint, and when.
//
// EINSEITIG, wie jeder Beleg in dieser Familie: ein Treffer beweist, dass die
// Nachricht gelesen wurde. Kein Treffer beweist NICHT, dass sie es nicht wurde
// -- das Buch beginnt erst mit dieser Fassung, ein älterer Hook-Schreiber trägt
// nichts ein, und die Grenze oben schneidet Altes ab. Ein Aufrufer darf daraus
// also "vermutlich noch nicht gelesen" ableiten und niemals "sicher nicht".
func PromptWasRead(instanceID, hash string, within time.Duration) (bool, time.Time) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return false, time.Time{}
	}
	f, err := os.Open(ReceiptLedgerPath(instanceID))
	if err != nil {
		return false, time.Time{}
	}
	defer func() { _ = f.Close() }()

	cutoff := time.Time{}
	if within > 0 {
		cutoff = time.Now().Add(-within)
	}
	var neuester time.Time
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 8192), 1<<20)
	for sc.Scan() {
		var e receiptEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Hash != hash {
			continue
		}
		at := time.Unix(e.Ts, 0)
		if !cutoff.IsZero() && at.Before(cutoff) {
			continue
		}
		if at.After(neuester) {
			neuester = at
		}
	}
	return !neuester.IsZero(), neuester
}

func trimReceiptLedger(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= receiptLedgerMax {
		return
	}
	kept := strings.Join(lines[len(lines)-receiptLedgerMax:], "\n") + "\n"
	tmp := path + ".tmp"
	if os.WriteFile(tmp, []byte(kept), 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}
