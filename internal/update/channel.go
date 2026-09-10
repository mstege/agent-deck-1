package update

import (
	"fmt"
	"strings"
)

// Diese Datei hält den Herkunftsschutz für Selbst-Updates.
//
// DAS VERSEHEN, GEGEN DAS SIE GEBAUT IST. Die Flotte läuft auf einem eigenen
// Fork mit Änderungen, die es upstream nicht gibt und nicht geben kann. Ein
// `agent-deck update` aus irgendeiner Sitzung holt aber ein Upstream-Release
// und ersetzt die laufende Binary damit — schweigend, erfolgreich, und mit dem
// Ergebnis, dass jede Härtung wieder weg ist. Bemerkt würde das erst, wenn eine
// Nachricht das nächste Mal verschwindet, und dann sieht es aus wie ein neuer
// Fehler statt wie ein zurückgedrehter alter.
//
// Deshalb ist die Vorgabe blockieren, nicht warnen. Eine Warnung, die
// vorbeirauscht, ist bei einem Vorgang, der die Binary tauscht, keine
// Schutzmaßnahme. Der bewusste Weg zu Upstream bleibt offen, aber er muss
// getippt werden: `--allow-upstream`.
//
// Gesetzt wird beides beim Bauen:
//
//	go build -ldflags "\
//	  -X github.com/asheshgoplani/agent-deck/internal/update.Channel=fleet \
//	  -X github.com/asheshgoplani/agent-deck/internal/update.RepoOverride=mstege/agent-deck-1"

// ChannelUpstream ist der Auslieferungskanal des öffentlichen Projekts.
const ChannelUpstream = "upstream"

// ChannelFleet kennzeichnet eine Binary, die aus unserem Fork gebaut wurde und
// Änderungen trägt, die in keinem Upstream-Release enthalten sind.
const ChannelFleet = "fleet"

// Channel wird beim Bauen gesetzt. Leer oder unbekannt heißt upstream — ein
// Build ohne Kennzeichnung ist kein Flottenbuild, und im Zweifel schützt der
// Schutz lieber nichts als das Falsche.
var Channel = ChannelUpstream

// AllowUpstreamInstall trägt die bewusste Entscheidung des Aufrufers bis zum
// Engpass durch. Die Sperre sitzt zusätzlich in PerformUpdate, also an der
// einzigen Stelle, die tatsächlich eine Binary ersetzt -- ein Schutz, der nur
// am CLI-Einstieg hängt, ist beim nächsten neuen Aufrufer wieder weg. Genau
// diese Klasse (zwei Codepfade, ein Fix) hat dieses Projekt schon einmal
// bezahlt.
var AllowUpstreamInstall bool

// RepoOverride verlegt die Release-Quelle auf ein anderes GitHub-Repo. Leer
// heißt: das Upstream-Repo.
var RepoOverride = ""

// IsFleetBuild meldet, ob diese Binary aus dem Fork der Flotte stammt.
func IsFleetBuild() bool { return strings.TrimSpace(Channel) == ChannelFleet }

// SourceRepo ist das Repo, aus dem Releases geholt werden. Alle Abfragen gehen
// hierüber, damit ein Flottenbuild seine eigenen Releases findet, sobald es
// welche gibt — und damit nicht eine Abfragestelle beim Umstellen vergessen
// werden kann.
func SourceRepo() string {
	if r := strings.TrimSpace(RepoOverride); r != "" {
		return r
	}
	return GitHubRepo
}

// ErrUpstreamWouldReplaceFleet ist der Text, den ein versehentliches Update zu
// sehen bekommt. Er nennt, was passieren würde, wie es richtig geht und wie man
// es trotzdem erzwingt — eine Sperre, die den Ausweg verschweigt, wird umgangen
// statt verstanden.
func ErrUpstreamWouldReplaceFleet() error {
	return fmt.Errorf(
		"diese Binary ist ein Flottenbuild und hat keinen Release-Kanal (Quelle waere %s).\n"+
			"  Ein Release von dort kennt die Haertung dieses Zweigs nicht und wuerde sie ersetzen —\n"+
			"  bemerkt wuerde das erst, wenn wieder eine Nachricht verschwindet.\n"+
			"  Richtig ist der eigene Weg:   fleet-update.sh\n"+
			"  Wirklich ein Release gewollt? agent-deck update --allow-upstream",
		SourceRepo())
}

// GuardUpstreamInstall entscheidet, ob ein Selbst-Update laufen darf.
//
// EIN FLOTTENBUILD HAT KEINEN RELEASE-KANAL, also ist Selbst-Update nicht sein
// Weg -- unabhängig davon, auf welches Repo die Quelle zeigt.
//
// Die erste Fassung war feiner gedacht und dadurch wirkungslos: sie blockierte
// nur, wenn die Quelle das Upstream-Repo war, und liess einen Flottenbuild mit
// gesetztem RepoOverride durch. Genau so baut unser Makefile aber. Der echte
// Lauf hat es gezeigt -- die Sperre schwieg, und `agent-deck update` starb
// stattdessen an "GitHub API returned status 404", weil der Fork keine Releases
// hat. Ein Schutz, der nur greift, weil eine Netzabfrage zufaellig scheitert,
// ist keiner: sobald auf dem Fork ein einziges Release liegt, waere der Weg
// wieder offen, ohne dass jemand etwas geaendert haette.
//
// Deshalb jetzt grob und richtig statt fein und falsch. Wenn es einmal echte
// Flotten-Releases gibt, wird diese Regel bewusst gelockert -- und der Test, der
// sie festhaelt, ist die Stelle, an der das auffaellt.
func GuardUpstreamInstall(allowUpstream bool) error {
	if allowUpstream || !IsFleetBuild() {
		return nil
	}
	return ErrUpstreamWouldReplaceFleet()
}
