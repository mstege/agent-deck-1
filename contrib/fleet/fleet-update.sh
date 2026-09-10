#!/bin/bash
# fleet-update.sh — der eigene Update-Weg der Flotte.
#
# WARUM NICHT `agent-deck update`. Das Kommando holt ein GitHub-RELEASE aus dem
# Upstream-Repo. Unsere Aenderungen sind dort nicht und koennen dort nicht hin --
# ein `update` wuerde die Haertung ersetzen, erfolgreich und schweigend. Die
# Binary traegt deshalb seit diesem Zweig eine Kennzeichnung, und `update`
# blockiert genau diesen Fall. Dieses Skript ist der Weg, der stattdessen gilt.
#
# Es baut aus dem Fork statt ein Release zu holen -- solange es auf dem Fork
# keine Releases gibt, ist das der einzige ehrliche Weg. Sobald es welche gibt,
# findet `agent-deck update` sie von selbst: die Quelle ist bereits umgebogen.
#
# Reihenfolge nach der Betriebsregel "Binary zuerst, Sessions danach":
# gesichert wird VOR dem Tausch, geprueft wird NACH dem Tausch, und laufende
# Sitzungen bleiben unberuehrt -- sie bekommen die neue Binary beim naechsten
# Aufruf, nicht mitten im Turn.
#
#   fleet-update.sh [--repo <pfad>] [--branch <name>] [--dry-run]

set -euo pipefail

REPO="${FLEET_REPO_PATH:-$HOME/Developer/oss/agent-deck}"
BRANCH="${FLEET_BRANCH:-feature/zustellung-haerten}"
ZIEL="${FLEET_INSTALL_PATH:-$HOME/.local/bin/agent-deck}"
dry=0

while [ $# -gt 0 ]; do
	case "$1" in
		--repo)    REPO="$2"; shift 2 ;;
		--branch)  BRANCH="$2"; shift 2 ;;
		--dry-run) dry=1; shift ;;
		-h|--help) sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*) echo "fleet-update: unbekannte Option $1" >&2; exit 2 ;;
	esac
done

[ -d "$REPO/.git" ] || { echo "fleet-update: $REPO ist kein Git-Checkout" >&2; exit 1; }

echo "== Stand =="
alt_version=$("$ZIEL" --version 2>/dev/null | head -1 || echo "keine Binary")
echo "  installiert: $alt_version"
echo "  Quelle:      $REPO ($BRANCH)"

worktree=$(git -C "$REPO" worktree list --porcelain 2>/dev/null \
	| awk -v b="refs/heads/$BRANCH" '/^worktree /{w=$2} /^branch /{if ($2==b) print w}' | head -1)
[ -n "$worktree" ] || worktree="$REPO"
echo "  Baumverz.:   $worktree"

if [ "$dry" = "1" ]; then echo "(--dry-run: nichts gebaut, nichts getauscht)"; exit 0; fi

echo "== Bauen =="
( cd "$worktree" && make fleet )
neu="$worktree/build/agent-deck"
[ -x "$neu" ] || { echo "fleet-update: Build hat keine Binary hinterlassen" >&2; exit 1; }

# Der neue Build wird geprueft, BEVOR der alte weicht. Eine Binary, die nicht
# einmal ihre Version nennen kann, darf die laufende nicht ersetzen.
neu_version=$("$neu" --version 2>/dev/null | head -1 || true)
[ -n "$neu_version" ] || { echo "fleet-update: neuer Build antwortet nicht auf --version" >&2; exit 1; }

echo "== Sichern =="
if [ -f "$ZIEL" ]; then
	sicherung="$ZIEL.bak-$(date +%Y%m%d%H%M%S)"
	cp "$ZIEL" "$sicherung"
	echo "  alte Binary: $sicherung"
	echo "  Rueckweg:    cp \"$sicherung\" \"$ZIEL\""
fi

echo "== Tauschen =="
# Erst daneben, dann atomar an die Stelle: ein abgebrochener Kopiervorgang darf
# nie eine halbe Binary hinterlassen, die die ganze Flotte benutzt.
cp "$neu" "$ZIEL.neu"
chmod +x "$ZIEL.neu"
mv "$ZIEL.neu" "$ZIEL"

echo "== Pruefen =="
echo "  $("$ZIEL" --version 2>&1 | head -1)"
if "$ZIEL" update --check >/dev/null 2>&1; then
	echo "  WARNUNG: 'update --check' lief durch — der Herkunftsschutz greift nicht!" >&2
	exit 1
fi
echo "  Herkunftsschutz aktiv: 'agent-deck update' wird blockiert."
echo
echo "Laufende Sitzungen behalten ihre alte Binary bis zum naechsten Aufruf — das ist so gewollt."
