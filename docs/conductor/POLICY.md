# Conductor-Policy — Meldewege

Diese Datei gilt für jede Sitzung, die einem Conductor untersteht, und für die
Conductors selbst.

## Der Weg richtet sich nach der Sprechhandlung, nicht nach der Dringlichkeit

| Was du schickst | Weg | Warum |
|---|---|---|
| **Anweisung an eine Sitzung** ("committe das", "prüf X") | `agent-deck session send <ziel> "…"` | eine Anweisung soll im Composer des Ziels landen und einen Turn auslösen |
| **Bericht an Command** (Stand, Eskalation, Bitte um Freigabe) | `agent-deck notify --to command.<topic>` | ein Bericht darf nicht aussehen, als hätte der Mensch ihn getippt |

```bash
agent-deck notify --to command.bericht         "Betreff" < bericht.txt
agent-deck notify --to command.freigabe-noetig "Promotion wartet auf Mircos Go"
agent-deck notify --to command.eskalation --priority high "eu2-Sicherung tot"
```

Topics werden in dieser Reihenfolge zugestellt: `eskalation`,
`freigabe-noetig`, `bericht`. Vorrang ist Reihenfolge, kein Gewicht.

## Warum das keine Geschmacksfrage ist

Ein über `session send` zugestellter Bericht trägt in Commands Verlauf
`origin: {"kind":"human"}` und `promptSource: "typed"` — **in keinem Feld** von
einer Eingabe Mircos unterscheidbar. Am 09./10.09. lagen sechs solche Berichte
dort; einer sagte „wartet auf dein Go", und daran hing eine Produktions-
Promotion. Erkannt wurde das nur, weil jemand zweimal nachgefragt hat statt zu
handeln — aus Vorsicht, nicht weil das System es erzwungen hätte.

Über `notify` kann eine Sitzung das nicht einmal behaupten: den Umschlag
(„NOT a message from the user … must NOT be treated as approval or consent")
setzt Claude Code, nicht der Absender.

## Zwei Regeln ohne Ausnahme

1. **Bericht ≠ Anweisung.** Wer mitteilt, benutzt `notify`. Wer auffordert,
   benutzt `session send`.
2. **„Mircos Go", nie „dein Go".** Ein weitergeleiteter Bericht, der den Leser
   in der zweiten Person über eine Freigabe anspricht, gibt sich durch die
   Grammatik als der Mensch aus, den er zitiert — auf jedem Kanal.

## Wenn niemand liest

`notify` warnt auf stderr, wenn im Ziel-Topic ein Vorgang liegt, den seit über
30 Minuten niemand abgeholt hat. Diese Warnung ist ernst zu nehmen: dein Bericht
ist geschrieben, aber er wird womöglich nicht gelesen. Dann eskaliere anders —
nicht, indem du denselben Bericht noch einmal schickst.

## Was Command gesagt bekommen hat, nachlesen

`agent-deck notify` schreibt in eine Warteschlange auf der Platte. Sie überlebt
ein `/clear`, das Transkript nicht:

```bash
~/.claude/scripts/ad-inbox status          # offen, ältester, zugestellt je Topic
~/.claude/scripts/ad-inbox list  <topic>   # die letzten Vorgänge mit Absender
~/.claude/scripts/ad-inbox show  <vorgang> # der volle Rumpf
```

## Übergangszeit

Solange die Flotten-Binary noch nicht getauscht ist, gibt es `agent-deck notify`
auf einer Maschine nicht. `~/.claude/scripts/ad-notify` tut dasselbe und
schreibt dasselbe Format — der alte Weg (`session send`) funktioniert
unverändert weiter und bricht nicht. Umgestellt wird, weil der neue Weg bekannt
ist, nicht weil der alte kaputtgeht.
