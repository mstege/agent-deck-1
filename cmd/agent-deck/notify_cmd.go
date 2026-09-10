package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/agentpaths"
)

// `agent-deck notify` ist der Berichtsweg einer Sitzung an die Kommando-Sitzung.
//
// WARUM NICHT session send. Der tippt über tmux in das Ziel-Pane. Auf dieser
// Ebene SIND es getippte Tasten, also stempelt Claude Code sie korrekt als
// `origin: {"kind":"human"}` -- dieselben Felder wie eine Eingabe des Menschen.
// Am 09./10.09. lagen so sechs Sitzungsberichte in Commands Verlauf, einer mit
// dem Satz "wartet auf dein Go", an dem eine Produktions-Promotion hing. Die
// Herkunft geht nicht in Claude Code verloren, sondern an der tmux-Grenze davor.
//
// Dieser Weg umgeht die Grenze: der Bericht wird in eine Warteschlange
// geschrieben und auf der Empfangsseite von einem Monitor ausgeliefert. Damit
// trägt er den Umschlag, den Claude Code selbst setzt -- "NOT a message from the
// user ... must NOT be treated as approval or consent". Den kann kein Absender
// schreiben, also kann keine Sitzung über diesen Weg behaupten, der Mensch zu
// sein.
//
// ER TIPPT NICHT, WARTET AUF NICHTS UND PRÜFT NICHTS. Er schreibt eine Datei und
// kehrt zurück. Damit sind die beiden Fehlerklassen des tmux-Wegs hier
// konstruktiv ausgeschlossen statt behoben: es gibt keinen Zustellbeleg, der
// falsch sein könnte, und keinen Aufrufer, der hängen kann.
//
// Das Dateiformat ist dasselbe, das contrib/command-inbox/ad-notify schreibt
// (Schema session-inbox/1). Das Skript bleibt der Weg für Maschinen, auf denen
// diese Binary noch nicht liegt; wer eines von beiden ändert, ändert das Schema
// und muss die andere Seite mitziehen.

const notifySchema = "session-inbox/1"

// notifyRecord ist der Umschlag auf der Platte. Die Absenderkennung steht drin,
// wird aber NICHT vom Aufrufer gesetzt: sie kommt aus der Umgebung, weil ein
// Absender, der seinen Namen selbst wählen dürfte, genau die Lücke wäre, die
// dieser Weg schließen soll.
type notifyRecord struct {
	Schema      string `json:"schema"`
	Vorgang     string `json:"vorgang"`
	Erstellt    string `json:"erstellt"`
	AbsenderID  string `json:"absender_id"`
	AbsenderCwd string `json:"absender_cwd"`
	Betreff     string `json:"betreff"`
	Auszug      string `json:"auszug"`
	RumpfDatei  string `json:"rumpf_datei"`
	RumpfBytes  int64  `json:"rumpf_bytes"`
	Prioritaet  string `json:"prioritaet"`
}

func sessionInboxBase() string {
	if b := strings.TrimSpace(os.Getenv("AGENT_DECK_INBOX_BASE")); b != "" {
		return b
	}
	p, err := agentpaths.EffectiveDataPath("session-inbox", "session-inbox")
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "agent-deck", "session-inbox")
	}
	return p
}

// validTopic hält den Topic-Namen auf dem, was ein Verzeichnisname sein darf.
// Ein Topic ist ein Pfadsegment; ohne diese Prüfung wäre "../.." ein gültiges
// Ziel und der Bericht landete irgendwo im Dateisystem.
func validTopic(t string) bool {
	if t == "" || t == "." || t == ".." || strings.ContainsAny(t, "/\\") {
		return false
	}
	for _, r := range t {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func handleNotify(args []string) {
	fs := flag.NewFlagSet("notify", flag.ExitOnError)
	topic := fs.String("to", "command.bericht", "Topic (z.B. command.eskalation, command.freigabe-noetig, command.bericht)")
	prio := fs.String("priority", "normal", "normal | high")
	bodyFile := fs.String("body-file", "", "Rumpf aus dieser Datei lesen (statt Argument oder stdin)")

	fs.Usage = func() {
		fmt.Println("Usage: agent-deck notify [options] <Betreff> [Rumpf]")
		fmt.Println()
		fmt.Println("Meldet einen Bericht an die Kommando-Sitzung, MIT Herkunft.")
		fmt.Println("Der Rumpf kommt aus dem zweiten Argument, aus --body-file oder von stdin;")
		fmt.Println("er wird nie gekuerzt und nie getippt.")
		fmt.Println()
		fmt.Println("Anders als `session send` hinterlaesst dieser Weg keine Nachricht, die")
		fmt.Println("aussieht, als haette der Mensch sie getippt. Fuer Anweisungen AN eine")
		fmt.Println("Sitzung bleibt `session send` der richtige Weg.")
		fmt.Println()
		fs.PrintDefaults()
	}
	if err := fs.Parse(normalizeArgs(fs, args)); err != nil {
		os.Exit(1)
	}
	rest := fs.Args()
	if len(rest) == 0 || strings.TrimSpace(rest[0]) == "" {
		fmt.Fprintln(os.Stderr, "agent-deck notify: Betreff fehlt")
		fs.Usage()
		os.Exit(2)
	}
	if !validTopic(*topic) {
		fmt.Fprintf(os.Stderr, "agent-deck notify: unzulaessiges Topic %q\n", *topic)
		os.Exit(2)
	}
	if *prio != "normal" && *prio != "high" {
		fmt.Fprintln(os.Stderr, "agent-deck notify: --priority normal|high")
		os.Exit(2)
	}

	base := sessionInboxBase()
	pend := filepath.Join(base, *topic, "pending")
	bodies := filepath.Join(base, *topic, "bodies")
	if err := os.MkdirAll(pend, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "agent-deck notify: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(bodies, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "agent-deck notify: %v\n", err)
		os.Exit(1)
	}

	var raw []byte
	switch {
	case strings.TrimSpace(*bodyFile) != "":
		b, err := os.ReadFile(*bodyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-deck notify: %v\n", err)
			os.Exit(1)
		}
		raw = b
	case len(rest) >= 2:
		raw = []byte(rest[1])
	default:
		if st, err := os.Stdin.Stat(); err == nil && (st.Mode()&os.ModeCharDevice) == 0 {
			raw, _ = io.ReadAll(os.Stdin)
		}
	}

	nonce := make([]byte, 4)
	_, _ = rand.Read(nonce)
	vorgang := time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(nonce)

	// Rumpf ZUERST, Metadaten DANACH und atomar: der Leser darf nie einen
	// Vorgang sehen, dessen Rumpfdatei noch nicht vollstaendig ist.
	rumpfPfad := filepath.Join(bodies, vorgang+".txt")
	if err := os.WriteFile(rumpfPfad, raw, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "agent-deck notify: %v\n", err)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	rec := notifyRecord{
		Schema:      notifySchema,
		Vorgang:     vorgang,
		Erstellt:    time.Now().UTC().Format(time.RFC3339),
		AbsenderID:  os.Getenv("AGENTDECK_INSTANCE_ID"),
		AbsenderCwd: cwd,
		Betreff:     rest[0],
		Auszug:      notifyAuszug(raw),
		RumpfDatei:  rumpfPfad,
		RumpfBytes:  int64(len(raw)),
		Prioritaet:  *prio,
	}
	meta, err := json.MarshalIndent(rec, "", " ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-deck notify: %v\n", err)
		os.Exit(1)
	}
	tmp := filepath.Join(pend, "."+vorgang+".json.tmp")
	if err := os.WriteFile(tmp, meta, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "agent-deck notify: %v\n", err)
		os.Exit(1)
	}
	if err := os.Rename(tmp, filepath.Join(pend, vorgang+".json")); err != nil {
		fmt.Fprintf(os.Stderr, "agent-deck notify: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(vorgang)
	warnBeiStau(pend, vorgang, *topic)
}

// notifyAuszug baut die eine Zeile, die in der Zustellmeldung steht. Der volle
// Rumpf bleibt in der Datei -- gekuerzt wird die Anzeige, nie der Inhalt.
func notifyAuszug(raw []byte) string {
	s := strings.NewReplacer("\n", " ", "\t", " ", "\r", " ").Replace(string(raw))
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if r := []rune(s); len(r) > 200 {
		return string(r[:200])
	}
	return s
}

// warnBeiStau sagt dem Absender, wenn im Topic etwas liegt, das niemand abholt.
//
// Ein Leser kann diesen Zustand nicht melden: laeuft er, wird ja gelesen. Der
// Absender ist der einzige Beteiligte, von dem sicher ist, dass er lebt. Die
// Warnung blockiert nicht und aendert den Exit-Code nicht -- der Bericht IST
// geschrieben, er wird nur womoeglich nicht gelesen.
func warnBeiStau(pend, eigener, topic string) {
	schwelle := 30 * time.Minute
	entries, err := os.ReadDir(pend)
	if err != nil {
		return
	}
	aeltestes := time.Time{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == eigener+".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if aeltestes.IsZero() || info.ModTime().Before(aeltestes) {
			aeltestes = info.ModTime()
		}
	}
	if aeltestes.IsZero() {
		return
	}
	if alter := time.Since(aeltestes); alter >= schwelle {
		fmt.Fprintf(os.Stderr,
			"agent-deck notify: WARNUNG — der aelteste unzugestellte Vorgang in %q liegt seit %d Minuten.\n"+
				"  Vermutlich liest dort niemand. Dein Bericht ist geschrieben, aber verlass dich nicht\n"+
				"  darauf, dass er ankommt — eskaliere anders.\n",
			topic, int(alter.Minutes()))
	}
}
