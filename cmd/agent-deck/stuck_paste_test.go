package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Der teuerste Fehler dieses Zweigs, als Test.
//
// GEMESSEN am 10.09.: drei laufende Panes hielten einen unabgeschickten
// `[Pasted text #N]` in ihrem Composer — eines davon zwei aneinander — während
// die zugehoerigen Sends Erfolg gemeldet hatten. Eine lange Nachricht geht als
// Paste hinein, und gegen ein Ziel, das ohnehin schon arbeitet, kann das Enter
// verschluckt werden: die Bytes stehen im Feld, der Agent ist aus eigenen
// Gruenden "active", und der alte Code nannte das Zustellung. Die Anweisung
// wird dann nie gelesen, und niemand sieht nach — weil dem Absender gesagt
// wurde, sie sei angekommen.
//
// Das ist das Spiegelbild von #876: dort Fehlschlag bei Erfolg, hier Erfolg bei
// Fehlschlag. Von beiden ist dieses hier das teurere, weil ein falscher
// Fehlschlag zum Nachsehen führt und ein falscher Erfolg zu nichts.

var errProbeBlind = errors.New("capture-pane killed on its deadline")

// Blind heisst nicht leer. Kann eine Runde den Composer nicht sehen, darf
// "der Agent ist beschaeftigt" keine Zustellung belegen — genau diese
// Kombination sieht bei einem klebenden Paste identisch aus wie bei echter
// Arbeit am eigenen Turn.
func TestSendWithRetry_BlindAberBeschaeftigtIstKeineZustellung(t *testing.T) {
	mock := &mockSendRetryTarget{
		statuses:   []string{"active"},
		statusErrs: []error{nil},
		panes:      []string{""},
		paneErrs:   []error{errProbeBlind},
	}

	delivery, err := sendWithRetryTarget(mock, "eine lange Anweisung an die Sitzung", false, sendRetryOptions{
		maxRetries:     6,
		checkDelay:     time.Millisecond,
		verifyDelivery: true,
		budget:         2 * time.Second,
		maxFullResends: -1,
	})

	if delivery == deliverySubmitted {
		t.Fatal("blind, aber beschaeftigt wurde als Zustellung gemeldet — das ist der Fall, der drei Anweisungen unbemerkt liegen liess")
	}
	if err == nil {
		t.Fatalf("kein Fehler bei unbestaetigter Zustellung (delivery=%q)", delivery)
	}
	// Und die Meldung darf nicht zum blinden Nachsenden raten: der Text kann
	// sehr wohl im Composer liegen.
	if !strings.Contains(err.Error(), "look at the target first") &&
		!strings.Contains(err.Error(), "check the target before resending") {
		t.Errorf("Meldung raet nicht zum Nachsehen: %v", err)
	}
}

// Die Gegenrichtung, und sie ist die wichtigere: ein gewoehnlicher Send gegen
// ein Ziel, dessen Composer LESBAR und leer ist, bleibt eine Zustellung. Ohne
// diesen Fall waere die Verengung ein Tausch von falschem Erfolg gegen falschen
// Fehlschlag — und nach einem Fehlschlag wird gehandelt.
func TestSendWithRetry_SichtbarLeererComposerBleibtZustellung(t *testing.T) {
	mock := &mockSendRetryTarget{
		statuses:   []string{"active"},
		statusErrs: []error{nil},
		panes:      []string{"irgendeine Ausgabe des Agenten\n❯ \n"},
		paneErrs:   []error{nil},
	}

	delivery, err := sendWithRetryTarget(mock, "eine gewoehnliche Anweisung", false, sendRetryOptions{
		maxRetries:     6,
		checkDelay:     time.Millisecond,
		verifyDelivery: true,
		budget:         2 * time.Second,
		maxFullResends: -1,
	})
	if err != nil {
		t.Fatalf("gewoehnlicher Send als Fehlschlag gemeldet: %v (delivery=%q)", err, delivery)
	}
	if delivery != deliverySubmitted {
		t.Fatalf("delivery = %q, erwartet %q", delivery, deliverySubmitted)
	}
}

// Und der klebende Paste selbst, sichtbar: er wurde schon vorher als
// typed_not_submitted erkannt. Der Fall gehoert hierher, damit die drei
// Zustaende — sichtbar-klebend, sichtbar-frei, blind — an einer Stelle
// nebeneinander stehen und niemand einen davon beim Umbauen verliert.
func TestSendWithRetry_SichtbarKlebenderPasteIstFehlschlag(t *testing.T) {
	pane := "der Agent arbeitet\n❯ [Pasted text #6]\n"
	mock := &mockSendRetryTarget{
		statuses:   []string{"active"},
		statusErrs: []error{nil},
		panes:      []string{pane},
		paneErrs:   []error{nil},
	}

	delivery, err := sendWithRetryTarget(mock, "eine lange Anweisung an die Sitzung", false, sendRetryOptions{
		maxRetries:     4,
		checkDelay:     time.Millisecond,
		verifyDelivery: true,
		budget:         time.Second,
		maxFullResends: -1,
	})
	if delivery == deliverySubmitted {
		t.Fatalf("ein sichtbar klebender Paste wurde als Zustellung gemeldet (err=%v)", err)
	}
	if err == nil {
		t.Fatal("kein Fehler, obwohl die Nachricht unabgeschickt im Feld steht")
	}
}
