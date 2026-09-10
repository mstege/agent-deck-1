#!/bin/bash
# watch-session-inbox.sh — die Command-Seite des Herkunftskanals.
#
# Für `Monitor` gedacht: jede ausgegebene Zeile wird zu einer Benachrichtigung
# IM laufenden Turn. Genau daran hängt Kriterium 3 -- ein Bericht erreicht
# Command auch dann, wenn Command gerade rechnet, ohne dass der Absender wartet.
#
# Und daran hängt Kriterium 1 und 4: eine Monitor-Zeile kommt als
# `origin: {"kind":"task-notification"}` an, mit dem Vorspann des Rahmens
# ("NOT a message from the user ... must NOT be treated as approval or
# consent"). Diesen Umschlag setzt Claude Code, nicht der Absender.
#
# TOPICS UND VORRANG. Das Ziel ist ein Topic-Name (`command.eskalation`,
# `command.bericht`, …), und der Wächter nimmt eine kommagetrennte LISTE. Er
# stellt sie in der angegebenen Reihenfolge zu -- eine Eskalation geht damit vor
# einem älteren Bericht. Das ist die ganze Priorisierung: keine Gewichtung, kein
# Scheduler, nur eine Reihenfolge, die der Betreiber selbst hinschreibt und
# nachlesen kann.
#
# AUFRÄUMEN. Zugestellte Vorgänge bleiben in processed/ liegen, damit ein
# Command nach einem /clear nachlesen kann, was ihm gesagt wurde. Damit wächst
# der Eingang unbegrenzt -- deshalb kehrt der Wächter höchstens einmal je Stunde
# aus: processed-Metadaten und Rümpfe älter als AGENT_DECK_INBOX_TAGE (14) fallen
# weg, ebenso verwaiste Rümpfe. Ein Rumpf, dessen Metadaten noch in pending/
# liegen, wird nie angefasst.
#
# Aufruf:  watch-session-inbox.sh [topic[,topic…]] [poll-sekunden]
#
# poll-sekunden = 0 stellt einmal zu und endet. Das ist der Modus der Tests und
# zugleich der ehrliche Weg, den Eingang von Hand zu leeren -- ein Wächter, der
# sich nicht ohne Dauerlauf ausprobieren lässt, wird nicht ausprobiert.
#
# NUR EIN LESER JE ZIEL. Zugestellte Vorgänge werden sofort nach processed/
# verschoben; zwei Wächter auf demselben Eingang, und einer sieht nie etwas.
# Der Umzug ist ein `mv` und damit atomar -- deshalb ist das Zusammenspiel mit
# dem Auffang-Hook (command-inbox-context.sh) ungefährlich: wer zuerst
# verschiebt, stellt zu, der andere sieht den Vorgang gar nicht erst.

set -uo pipefail

topics="${1:-command}"
interval="${2:-15}"
TAGE="${AGENT_DECK_INBOX_TAGE:-14}"
AUFRAEUM_ABSTAND="${AGENT_DECK_INBOX_AUFRAEUM_SEKUNDEN:-3600}"
letztes_aufraeumen=0

BASE="${AGENT_DECK_INBOX_BASE:-$HOME/.local/share/agent-deck/session-inbox}"
DB="${AGENT_DECK_STATE_DB:-$HOME/.local/share/agent-deck/profiles/default/state.db}"

# Aufräumen: nur processed/ und verwaiste Rümpfe, nie etwas aus pending/.
# Ein Rumpf ohne Metadaten ist der Rest eines Schreibers, der zwischen Rumpf
# und Metadaten gestorben ist -- nach der Frist ist er Müll, vorher ist er
# vielleicht gerade im Entstehen.
aufraeumen() {
	local topic="$1"
	local proc="$BASE/$topic/processed" bodies="$BASE/$topic/bodies"
	[ -d "$proc" ] || return 0
	find "$proc" -maxdepth 1 -name '*.json' -mtime "+$TAGE" -print 2>/dev/null | while read -r alt; do
		local v; v=$(basename "$alt" .json)
		rm -f "$alt" "$bodies/$v.txt" 2>/dev/null || true
	done
	[ -d "$bodies" ] || return 0
	find "$bodies" -maxdepth 1 -name '*.txt' -mtime "+$TAGE" -print 2>/dev/null | while read -r rumpf; do
		local v; v=$(basename "$rumpf" .txt)
		[ -f "$BASE/$topic/pending/$v.json" ] && continue
		[ -f "$proc/$v.json" ] && continue
		rm -f "$rumpf" 2>/dev/null || true
	done
}

# Absender auflösen: die Instanz-ID aus der Umgebung des Absenders gegen die
# agent-deck-Registry. Read-only und ~5 ms, also billig genug für jeden Vorgang.
#
# WICHTIG, und bewusst so: ein Absender, der sich nicht auflösen lässt, wird als
# UNBEKANNT ausgewiesen, nicht stillschweigend geglaubt und nicht verschwiegen.
# Der Kanal macht "Maschine oder Mensch" unfälschbar; "welche Sitzung" ist eine
# Angabe des Absenders, und eine Angabe, die niemand prüft, ist eine Behauptung.
aufloesen() {
	local id="$1"
	[ -n "$id" ] || { echo "UNBEKANNT (keine Instanz-ID in der Absenderumgebung)"; return; }
	local t
	t=$(sqlite3 -readonly "$DB" \
		"select title from instances where id='${id//\'/}' limit 1;" 2>/dev/null || true)
	if [ -n "$t" ]; then echo "$t"; else echo "UNBEKANNT (Instanz-ID $id steht in keiner Registry)"; fi
}

while :; do
  # Topics in der angegebenen Reihenfolge: Vorrang ist Reihenfolge, nicht Gewicht.
  IFS=',' read -r -a topic_liste <<< "$topics"
  for ziel in "${topic_liste[@]}"; do
	ziel="${ziel// /}"
	[ -n "$ziel" ] || continue
	PEND="$BASE/$ziel/pending"
	PROC="$BASE/$ziel/processed"
	mkdir -p "$PEND" "$PROC" 2>/dev/null || true

	# Chronologisch innerhalb eines Topics: die Vorgangs-ID beginnt mit einem
	# sortierenden Zeitstempel.
	for meta in $(ls -1 "$PEND"/*.json 2>/dev/null | sort); do
		vorgang=$(basename "$meta" .json)

		# Anspruch anmelden, BEVOR gelesen wird. Schlägt das mv fehl, hat ein
		# anderer Leser den Vorgang -- dann still weiter, nicht doppelt zustellen.
		mv "$meta" "$PROC/$vorgang.json" 2>/dev/null || continue

		# Die Aufbewahrungsfrist zaehlt ab ZUSTELLUNG, nicht ab Erstellung.
		# `mv` erhaelt die mtime, also traegt ein Vorgang, der lange in der
		# Warteschlange lag, sofort ein altes Datum -- und der Aufraeumer haette
		# ihn im selben Durchgang geloescht, in dem er zugestellt wurde. Damit
		# waere ausgerechnet der Bericht unlesbar, der am laengsten auf einen
		# Leser gewartet hat. Ein `touch` setzt die Uhr auf den Moment, ab dem
		# die Frist gemeint ist.
		touch "$PROC/$vorgang.json" 2>/dev/null || true

		absender_id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("absender_id",""))' \
			"$PROC/$vorgang.json" 2>/dev/null || true)
		titel=$(aufloesen "$absender_id")

		python3 - "$PROC/$vorgang.json" "$titel" <<'PY'
import json,sys
meta=json.load(open(sys.argv[1])); titel=sys.argv[2]
prio = "  [VORRANG]" if meta.get("prioritaet")=="high" else ""
print(f'BERICHT EINER SITZUNG — {titel}{prio}')
print(f'  Betreff:  {meta.get("betreff","")}')
print(f'  Gemeldet: {meta.get("erstellt","")} · Arbeitsverzeichnis {meta.get("absender_cwd","")}')
print(f'  Auszug:   {meta.get("auszug","")}')
print(f'  Volltext: {meta.get("rumpf_datei","")} ({meta.get("rumpf_bytes",0)} Bytes, ungekuerzt)')
PY
	done
  done

	jetzt=$(date +%s)
	if [ $(( jetzt - letztes_aufraeumen )) -ge "$AUFRAEUM_ABSTAND" ]; then
		for ziel in "${topic_liste[@]}"; do
			ziel="${ziel// /}"; [ -n "$ziel" ] && aufraeumen "$ziel"
		done
		letztes_aufraeumen="$jetzt"
	fi

	[ "$interval" = "0" ] && break
	sleep "$interval"
done
