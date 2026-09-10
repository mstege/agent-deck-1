package update

import (
	"strings"
	"testing"
)

// Der Herkunftsschutz hat genau eine Aufgabe: verhindern, dass ein
// Upstream-Release eine Binary ersetzt, die Aenderungen traegt, welche upstream
// nicht existieren. Diese Faelle halten die Richtung fest -- ein Schutz, der zu
// viel blockiert, wird abgeschaltet, und einer, der zu wenig blockiert, ist
// keiner.

func mitKanal(t *testing.T, kanal, repo string) {
	t.Helper()
	altK, altR, altA := Channel, RepoOverride, AllowUpstreamInstall
	t.Cleanup(func() { Channel, RepoOverride, AllowUpstreamInstall = altK, altR, altA })
	Channel, RepoOverride, AllowUpstreamInstall = kanal, repo, false
}

func TestGuardUpstreamInstall(t *testing.T) {
	tests := []struct {
		name    string
		kanal   string
		repo    string
		erlaubt bool
		blockt  bool
	}{
		{
			name:  "Flottenbuild gegen Upstream — der Fall, um den es geht",
			kanal: ChannelFleet, repo: "", blockt: true,
		},
		{
			name: "Flottenbuild mit gesetztem Fork — der Fall, den die erste Fassung durchliess",
			// So baut unser Makefile. Die erste Fassung hielt das fuer
			// unbedenklich; der echte Lauf starb dann an einem 404, weil der
			// Fork keine Releases hat. Sobald dort eines liegt, waere der Weg
			// offen gewesen. Ein Flottenbuild hat keinen Release-Kanal, Punkt.
			kanal: ChannelFleet, repo: "mstege/agent-deck-1", blockt: true,
		},
		{
			name: "Upstream-Build bleibt unberuehrt",
			// Wichtig fuer die Gegenrichtung: der Schutz darf das oeffentliche
			// Projekt nicht lahmlegen, sonst ist er in dem Moment weg, in dem
			// jemand ihn als stoerend empfindet.
			kanal: ChannelUpstream, repo: "", blockt: false,
		},
		{
			name:  "ein ungekennzeichneter Build gilt als Upstream",
			kanal: "", repo: "", blockt: false,
		},
		{
			name:  "die bewusste Entscheidung kommt durch",
			kanal: ChannelFleet, repo: "", erlaubt: true, blockt: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mitKanal(t, tc.kanal, tc.repo)
			err := GuardUpstreamInstall(tc.erlaubt)
			if tc.blockt && err == nil {
				t.Fatal("Update lief durch, obwohl es die Haertung ersetzt haette")
			}
			if !tc.blockt && err != nil {
				t.Fatalf("Update blockiert, obwohl nichts verloren ginge: %v", err)
			}
		})
	}
}

// Die Meldung muss den Ausweg nennen. Eine Sperre, die nur "nein" sagt, wird
// umgangen statt verstanden -- dann steht am Ende doch das Upstream-Release auf
// der Platte, nur mit mehr Umwegen davor.
func TestSperrmeldungNenntBeideWege(t *testing.T) {
	mitKanal(t, ChannelFleet, "")
	err := GuardUpstreamInstall(false)
	if err == nil {
		t.Fatal("kein Fehler")
	}
	for _, muss := range []string{"fleet-update.sh", "--allow-upstream", "Flottenbuild"} {
		if !strings.Contains(err.Error(), muss) {
			t.Errorf("Meldung nennt %q nicht: %s", muss, err.Error())
		}
	}
}

// Die Sperre sitzt am Engpass, nicht nur am CLI-Einstieg. Ohne diesen Fall
// koennte ein neuer Aufrufer PerformUpdate direkt benutzen und den Schutz
// unbemerkt umgehen.
func TestPerformUpdateBlocktAmEngpass(t *testing.T) {
	mitKanal(t, ChannelFleet, "")
	err := PerformUpdate("https://example.invalid/agent-deck.tar.gz")
	if err == nil {
		t.Fatal("PerformUpdate lief an der Sperre vorbei")
	}
	if !strings.Contains(err.Error(), "fleet-update.sh") {
		t.Fatalf("PerformUpdate scheiterte an etwas anderem als der Sperre: %v", err)
	}
}

// SourceRepo ist die einzige Stelle, ueber die Release-Abfragen laufen. Bricht
// das, holt ein Flottenbuild wieder Upstream-Releases -- leise.
func TestSourceRepoFolgtDemOverride(t *testing.T) {
	mitKanal(t, ChannelFleet, "mstege/agent-deck-1")
	if got := SourceRepo(); got != "mstege/agent-deck-1" {
		t.Fatalf("SourceRepo() = %q, erwartet den Fork", got)
	}
	mitKanal(t, ChannelUpstream, "")
	if got := SourceRepo(); got != GitHubRepo {
		t.Fatalf("SourceRepo() = %q, erwartet %q", got, GitHubRepo)
	}
}
