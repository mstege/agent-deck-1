package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Die Verteilungsweiche in main() ist zweimal auf dieselbe Weise kaputtgegangen.
//
// Beim ersten Mal (Umweg U6) fehlte das `default`, und jedes unbekannte Token
// fiel in den TUI-Start. Beim zweiten Mal fehlte hinter einem neuen `case` das
// `return` -- das Kommando lief korrekt durch und danach lief zusaetzlich der
// TUI-Start, der in einer agent-deck-Sitzung mit einer Fehlermeldung abbricht.
// Beide Male war die Ursache dieselbe: Go faellt nicht durch, aber die Funktion
// laeuft weiter, und ein `case` ohne `return` ist optisch nicht von einem mit zu
// unterscheiden.
//
// Dieser Test liest die Weiche selbst und verlangt, dass jeder Zweig sie
// verlaesst. Er prueft eine Struktur, kein Verhalten -- absichtlich: Verhalten
// muesste man je Kommando nachstellen, und genau das hat beim zweiten Mal
// niemand getan.
func TestJederDispatchZweigVerlaesstMain(t *testing.T) {
	fset := token.NewFileSet()
	datei, err := parser.ParseFile(fset, "main.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("main.go nicht lesbar: %v", err)
	}

	var mainFunc *ast.FuncDecl
	for _, d := range datei.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "main" && fn.Recv == nil {
			mainFunc = fn
			break
		}
	}
	if mainFunc == nil {
		t.Fatal("func main() nicht gefunden")
	}

	// Die Weiche ist der Switch mit den meisten Zweigen -- jede andere
	// Verzweigung in main() ist klein. Ihn ueber die Groesse zu finden ist
	// robuster, als ihn ueber eine Zeilennummer zu suchen, die sich mit jedem
	// Commit verschiebt.
	var weiche *ast.SwitchStmt
	ast.Inspect(mainFunc, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok || sw.Body == nil {
			return true
		}
		if weiche == nil || len(sw.Body.List) > len(weiche.Body.List) {
			weiche = sw
		}
		return true
	})
	if weiche == nil || len(weiche.Body.List) < 10 {
		t.Fatal("die Kommando-Weiche in main() wurde nicht erkannt")
	}

	// Ein Zweig darf durchfallen -- aber nur ausgesprochen. `web` tut das seit
	// jeher, um den TUI-Start dahinter zu benutzen, und sagt es im Kommentar.
	// Ein Versehen sieht genauso aus wie eine Absicht; der Unterschied ist, dass
	// die Absicht aufgeschrieben wurde.
	roh, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("main.go nicht lesbar: %v", err)
	}
	quelle := string(roh)

	for i, stmt := range weiche.Body.List {
		zweig, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		naechste := token.NoPos
		if i+1 < len(weiche.Body.List) {
			naechste = weiche.Body.List[i+1].Pos()
		}
		if erlaubtDurchfall(quelle, fset, zweig, naechste) {
			continue
		}
		if len(zweig.Body) == 0 {
			continue // ein leerer Zweig faellt in nichts hinein
		}
		letzte := zweig.Body[len(zweig.Body)-1]
		switch s := letzte.(type) {
		case *ast.ReturnStmt:
		case *ast.BranchStmt:
			// break/fallthrough sind bewusste Entscheidungen, kein Versehen.
		case *ast.ExprStmt:
			if istExitAufruf(s.X) {
				continue
			}
			t.Errorf("%s: Zweig %s endet nicht mit return — der Aufruf laeuft danach in den TUI-Start",
				fset.Position(letzte.Pos()), zweigName(zweig))
		default:
			t.Errorf("%s: Zweig %s endet nicht mit return — der Aufruf laeuft danach in den TUI-Start",
				fset.Position(letzte.Pos()), zweigName(zweig))
		}
	}

	// Umweg U6 wurde nicht mit einem `default` behoben, sondern mit einer
	// Registry-Pruefung VOR der Weiche: ein unbekanntes Token wird abgelehnt,
	// bevor es irgendwo hineinfallen kann. Diese Pruefung ist der Schutz, also
	// wird sie festgehalten -- nicht die Bauform, die sie ersetzt hat.
	if !erwaehnt(mainFunc, "commandRegistry") {
		t.Error("die Registry-Pruefung vor der Weiche ist weg — ein unbekanntes Token faellt wieder in den TUI-Start (Umweg U6)")
	}
}

// erlaubtDurchfall meldet, ob dieser Zweig sein Durchfallen ausdruecklich
// begruendet. Gelesen wird der Quelltext des Zweigs, nicht die
// Kommentarzuordnung des Parsers: ein Kommentar am Ende eines Zweigs haengt
// dort syntaktisch schon am naechsten `case`, und ein Test, der an dieser
// Feinheit scheitert, prueft den Parser statt den Code.
func erlaubtDurchfall(quelle string, fset *token.FileSet, zweig *ast.CaseClause, naechste token.Pos) bool {
	von := fset.Position(zweig.Colon).Offset
	bis := len(quelle)
	if naechste.IsValid() {
		bis = fset.Position(naechste).Offset
	}
	if von < 0 || bis > len(quelle) || von >= bis {
		return false
	}
	t := strings.ToLower(quelle[von:bis])
	return strings.Contains(t, "fall through") || strings.Contains(t, "faellt durch")
}

func erwaehnt(fn *ast.FuncDecl, name string) bool {
	gefunden := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			gefunden = true
			return false
		}
		return true
	})
	return gefunden
}

func istExitAufruf(x ast.Expr) bool {
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "os" && sel.Sel.Name == "Exit"
}

func zweigName(z *ast.CaseClause) string {
	if len(z.List) == 0 {
		return "default"
	}
	if lit, ok := z.List[0].(*ast.BasicLit); ok {
		return lit.Value
	}
	return "?"
}
