package send

import (
	"crypto/sha256"
	"encoding/hex"
)

// PromptDigest ist der Fingerabdruck einer Nachricht, an dem beide Enden sie
// wiedererkennen.
//
// WOZU. Der Empfangsbeleg aus Claudes Hook belegt bisher nur eine FLANKE: "ein
// Prompt wurde angenommen", nicht "dieser". Das reicht, solange nur eine
// Nachricht unterwegs ist, und es reicht nicht, sobald zwei Absender dasselbe
// Ziel bedienen oder ein Turn eine eingereihte Nachricht aufnimmt, während wir
// noch auf unsere warten. Am 10.09. wurde gemessen, dass Claude Code den
// Prompt-Text im Klartext an den UserPromptSubmit-Hook durchreicht — damit ist
// die Frage "wurde MEINE Nachricht gelesen" beantwortbar statt geschätzt.
//
// WARUM HIER. Absender und Empfänger müssen denselben Wert bilden, sonst ist
// der Vergleich wertlos und der Fehler unsichtbar: er sieht aus wie eine
// Nachricht, die nie ankam. Deshalb liegt die Berechnung an einer Stelle, in
// demselben Paket, das schon die Prompt-Normalisierung beider Pfade hält.
//
// NORMALISIERT, WEIL DER WEG NICHT BYTETREU IST. Zwischen dem, was der Absender
// tippt, und dem, was Claude als Prompt meldet, liegt ein Terminal: abschließende
// Zeilenumbrüche, umgebrochene Zeilen, NBSP aus einem kopierten Text. Ein
// Vergleich auf rohen Bytes würde daran scheitern und einen gelesenen Prompt als
// ungelesen melden — die teuerste Richtung. NormalizePromptText ist dieselbe
// Funktion, mit der der Composer-Abgleich schon arbeitet.
//
// EINSEITIG, WIE JEDER BELEG IN DIESER DATEI-FAMILIE. Ein Treffer beweist, dass
// genau diese Nachricht als Prompt angenommen wurde. Ein Fehltreffer beweist
// nichts: ein älterer Hook-Schreiber legt gar keinen Fingerabdruck ab, und ein
// Werkzeug ohne Hooks erst recht nicht. Deshalb darf ein fehlender Treffer nie
// einen Fehlschlag begründen, sondern nur den vorhandenen Flankenbeleg nicht
// verstärken.
func PromptDigest(message string) string {
	normalized := NormalizePromptText(message)
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	// 16 Hex-Zeichen sind 64 Bit. Das ist kein Sicherheitsmerkmal, sondern eine
	// Wiedererkennung unter Nachrichten, die dieselbe Sitzung in derselben
	// Minute erreichen — dort ist eine zufällige Kollision so unwahrscheinlich,
	// dass die kürzere Datei und die lesbare Logzeile mehr wert sind.
	return hex.EncodeToString(sum[:8])
}
