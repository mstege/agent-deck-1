#!/bin/bash
# test-session-inbox.sh — die Zusagen des Herkunftskanals, als Prüfungen.
#
# Jeder Fall hier hält eine Zusage fest, die in einem Befund vom 09./10.09. schon
# einmal gebrochen war. Ein Test, der nur "es läuft" prüft, hätte keinen davon
# gefunden.

set -uo pipefail
cd "$(dirname "$0")"
HIER="$PWD"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

export AGENT_DECK_INBOX_BASE="$TMP/inbox"
export AGENT_DECK_STATE_DB="$TMP/state.db"
export AGENT_DECK_INBOX_COMMAND_CWD="$TMP/command"
mkdir -p "$TMP/command"

sqlite3 "$AGENT_DECK_STATE_DB" \
	"create table instances (id text primary key, title text);
	 insert into instances values ('inst-echt-1','mnm-orchestration');"

fehler=0
ok()   { printf '  ok    %s\n' "$1"; }
fail() { printf '  FEHL  %s\n' "$1"; fehler=$((fehler+1)); }
pruefe() { if [ "$2" = "$3" ]; then ok "$1"; else fail "$1 (erwartet '$3', bekam '$2')"; fi; }

echo "== 1. Der Schreiber blockiert nicht und kuerzt nicht (Kriterium 2, Befund 9) =="
# 200 KB, weit jenseits dessen, was der tmux-Weg unzerteilt durchlaesst.
python3 -c "print('Z' * 200000)" > "$TMP/gross.txt"
start=$(date +%s)
vorgang=$(AGENTDECK_INSTANCE_ID=inst-echt-1 ./ad-notify "Grossmeldung" < "$TMP/gross.txt")
dauer=$(( $(date +%s) - start ))
[ "$dauer" -le 2 ] && ok "kehrt in ${dauer}s zurueck" || fail "brauchte ${dauer}s — ein Schreiber darf nie warten"
bytes=$(wc -c < "$AGENT_DECK_INBOX_BASE/command/bodies/$vorgang.txt" | tr -d ' ')
pruefe "Rumpf ungekuerzt, samt Zeilenumbruch" "$bytes" "200001"

echo "== 2. Die Zustellzeile nennt die aufgeloeste Absendersitzung (Kriterium 1) =="
aus=$(./watch-session-inbox.sh command 0)
case "$aus" in
	*"BERICHT EINER SITZUNG — mnm-orchestration"*) ok "Absender aus der Registry aufgeloest" ;;
	*) fail "Absender fehlt oder ist falsch: $aus" ;;
esac
case "$aus" in *"Grossmeldung"*) ok "Betreff steht in der Zeile" ;; *) fail "Betreff fehlt" ;; esac
case "$aus" in *"200001 Bytes, ungekuerzt"*) ok "Verweis auf den Volltext mit Groesse" ;; *) fail "Volltext-Verweis fehlt" ;; esac

echo "== 3. Zugestellt heisst verschoben — kein Vorgang zweimal =="
pruefe "pending leer" "$(ls -1 "$AGENT_DECK_INBOX_BASE/command/pending" 2>/dev/null | wc -l | tr -d ' ')" "0"
pruefe "processed traegt ihn" "$(ls -1 "$AGENT_DECK_INBOX_BASE/command/processed" | wc -l | tr -d ' ')" "1"
zweit=$(./watch-session-inbox.sh command 0)
pruefe "zweiter Lauf stellt nichts erneut zu" "$zweit" ""

echo "== 4. Ein unbekannter Absender wird ausgewiesen, nicht geglaubt (Kriterium 4) =="
AGENTDECK_INSTANCE_ID=inst-erfunden-9 ./ad-notify "Fremdmeldung" "Rumpf" >/dev/null
aus=$(./watch-session-inbox.sh command 0)
case "$aus" in
	*"UNBEKANNT (Instanz-ID inst-erfunden-9 steht in keiner Registry)"*) ok "unbekannte ID benannt" ;;
	*) fail "unbekannte ID nicht ausgewiesen: $aus" ;;
esac

echo "== 5. Fehlt die Kennung ganz, wird das gesagt statt verschwiegen =="
( unset AGENTDECK_INSTANCE_ID; ./ad-notify "Anonym" "Rumpf" >/dev/null )
aus=$(./watch-session-inbox.sh command 0)
case "$aus" in
	*"UNBEKANNT (keine Instanz-ID in der Absenderumgebung)"*) ok "fehlende Kennung benannt" ;;
	*) fail "fehlende Kennung verschwiegen: $aus" ;;
esac

echo "== 6. Zwei Leser, jede Nachricht genau einmal (Wächter + Auffang-Hook) =="
for i in 1 2 3 4 5; do
	AGENTDECK_INSTANCE_ID=inst-echt-1 ./ad-notify "Serie $i" "Rumpf $i" >/dev/null
done
a=$(./watch-session-inbox.sh command 0 & \
    printf '{"cwd":"%s"}' "$TMP/command" | ./session-inbox-context.sh & wait)
zahl=$(printf '%s' "$a" | grep -c 'BERICHT EINER SITZUNG' || true)
pruefe "fuenf Vorgaenge, fuenf Zustellungen" "$zahl" "5"
pruefe "pending danach leer" "$(ls -1 "$AGENT_DECK_INBOX_BASE/command/pending" 2>/dev/null | wc -l | tr -d ' ')" "0"

echo "== 7. Der Auffang-Hook leert NUR den Eingang seines Ziels =="
AGENTDECK_INSTANCE_ID=inst-echt-1 ./ad-notify "Nicht fuer dich" "Rumpf" >/dev/null
fremd=$(printf '{"cwd":"/Users/mstege/Developer/business/mnemo"}' | ./session-inbox-context.sh)
pruefe "fremde Sitzung bekommt nichts" "$fremd" ""
pruefe "Vorgang liegt noch im Eingang" "$(ls -1 "$AGENT_DECK_INBOX_BASE/command/pending" | wc -l | tr -d ' ')" "1"

echo "== 8. Der Auffang-Hook sagt dazu, dass es keine Freigabe ist =="
eigen=$(printf '{"cwd":"%s"}' "$TMP/command" | ./session-inbox-context.sh)
case "$eigen" in
	*"keiner davon ist eine Freigabe"*) ok "Warnung steht im Auffangnetz" ;;
	*) fail "Warnung fehlt: $eigen" ;;
esac

echo "== 9. Nie ein Vorgang ohne fertigen Rumpf (Reihenfolge beim Schreiben) =="
AGENTDECK_INSTANCE_ID=inst-echt-1 ./ad-notify "Ordnung" "Rumpf" >/dev/null
verwaist=0
for m in "$AGENT_DECK_INBOX_BASE/command/pending"/*.json; do
	rd=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["rumpf_datei"])' "$m")
	[ -f "$rd" ] || verwaist=$((verwaist+1))
done
pruefe "keine verwaisten Metadaten" "$verwaist" "0"

echo
if [ "$fehler" = "0" ]; then echo "ALLE PRUEFUNGEN GRUEN"; else echo "$fehler PRUEFUNG(EN) ROT"; fi
exit "$fehler"
