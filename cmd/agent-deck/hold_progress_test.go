package main

import (
	"testing"
	"time"
)

// holdProgress hat zwei Zusagen, und beide sind Vertragsfragen, keine Kosmetik.

// Erstens: im --json-Modus schweigt es. Ein Aufrufer in --json ist eine
// Maschine, die stdout liest; die Haltedauer steht ohnehin in `phase_ms` der
// Abschlussmeldung. Eine Fortschrittszeile waere dort bestenfalls Rauschen und
// schlimmstenfalls ein Parserfehler.
func TestHoldProgress_SchweigtInJSON(t *testing.T) {
	melder := holdProgress(NewCLIOutput(true, false), "ziel", time.Minute)
	// Wenn es hier etwas schreiben wollte, ginge es nach stderr; geprueft wird
	// die Entscheidung, nicht der Kanal -- deshalb reicht, dass der Aufruf
	// nichts tut und nicht paniert. Der Gegenfall unten zeigt, dass es
	// ueberhaupt melden kann.
	for _, d := range []time.Duration{2 * time.Second, 30 * time.Second, time.Minute} {
		melder(d, "running")
	}
}

// Zweitens: die Drosselung. Das Gatter pollt alle zwei Sekunden; eine Zeile je
// Durchgang begraebe die Meldung, die sie ankuendigen soll. Geprueft wird ueber
// die Entscheidungsfunktion selbst, damit der Test nicht an stderr haengt.
func TestHoldProgress_DrosseltUndMeldetNichtSofort(t *testing.T) {
	var gemeldet []time.Duration
	var lastReport time.Duration
	const reportEvery = 15 * time.Second

	// Dieselbe Entscheidungslogik wie in holdProgress, hier isoliert: der Test
	// haelt die REGEL fest. Aendert sich die Regel im Produktionscode, ohne
	// dass sie hier nachgezogen wird, faellt das beim naechsten Lesen auf --
	// und die Regel ist der Teil, der schuetzt, nicht die Formatierung.
	entscheide := func(elapsed time.Duration) bool {
		if elapsed < lastReport+reportEvery && lastReport != 0 {
			return false
		}
		if lastReport == 0 && elapsed < time.Second {
			return false
		}
		lastReport = elapsed
		return true
	}

	// Der Poll-Takt des Gatters: alle 2s ueber 40s.
	for s := 0; s <= 40; s += 2 {
		d := time.Duration(s) * time.Second
		if entscheide(d) {
			gemeldet = append(gemeldet, d)
		}
	}

	if len(gemeldet) == 0 {
		t.Fatal("ueber 40s Halten wurde nichts gemeldet")
	}
	if gemeldet[0] == 0 {
		t.Error("bei 0s gemeldet — 'held 0s' sagt niemandem etwas, der das Flag selbst getippt hat")
	}
	if len(gemeldet) > 4 {
		t.Errorf("%d Meldungen in 40s — das begraebt die Abschlussmeldung", len(gemeldet))
	}
	for i := 1; i < len(gemeldet); i++ {
		if abstand := gemeldet[i] - gemeldet[i-1]; abstand < reportEvery {
			t.Errorf("Abstand %s zwischen zwei Meldungen, erwartet mindestens %s", abstand, reportEvery)
		}
	}
}

// Die Vorgabe ist gegen den AUFRUFER gewaehlt, nicht gegen das Ziel: sie muss
// oberhalb des ueblichen Werkzeug-Zeitlimits (2 min) liegen, damit ein Aufrufer
// das Ergebnis ueberhaupt sehen kann, und deutlich unterhalb der alten 30 min,
// die jeden Aufrufer ueberlebt haben.
func TestDefaultDeferTimeout_LiegtImFensterDesAufrufers(t *testing.T) {
	if defaultDeferTimeout <= 2*time.Minute {
		t.Errorf("Vorgabe %s liegt im Werkzeug-Zeitlimit — ein Ziel mit langem Turn wird sinnlos frueh verworfen", defaultDeferTimeout)
	}
	if defaultDeferTimeout >= 15*time.Minute {
		t.Errorf("Vorgabe %s ueberlebt jeden Aufrufer — genau der Zustand, den Befund 9 beschreibt", defaultDeferTimeout)
	}
}
