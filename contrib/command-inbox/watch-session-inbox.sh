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
# Aufruf:  watch-session-inbox.sh [ziel] [poll-sekunden]
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

ziel="${1:-command}"
interval="${2:-15}"

BASE="${AGENT_DECK_INBOX_BASE:-$HOME/.local/share/agent-deck/session-inbox}"
DB="${AGENT_DECK_STATE_DB:-$HOME/.local/share/agent-deck/profiles/default/state.db}"
PEND="$BASE/$ziel/pending"
PROC="$BASE/$ziel/processed"

mkdir -p "$PEND" "$PROC" 2>/dev/null || true

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
	# Chronologisch: die Vorgangs-ID beginnt mit einem sortierenden Zeitstempel.
	for meta in $(ls -1 "$PEND"/*.json 2>/dev/null | sort); do
		vorgang=$(basename "$meta" .json)

		# Anspruch anmelden, BEVOR gelesen wird. Schlägt das mv fehl, hat ein
		# anderer Leser den Vorgang -- dann still weiter, nicht doppelt zustellen.
		mv "$meta" "$PROC/$vorgang.json" 2>/dev/null || continue

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
	[ "$interval" = "0" ] && break
	sleep "$interval"
done
