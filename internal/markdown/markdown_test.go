package markdown

import (
	"regexp"
	"strings"
	"testing"
)

func r(src string) string { return string(Render(src, Options{})) }

func TestRenderLegacyPlainText(t *testing.T) {
	got := r("Riga uno\nriga due\n\nVedi https://example.org o scrivi a ced@example.org")
	for _, want := range []string{
		"<p>Riga uno<br>\nriga due</p>",
		`<a href="https://example.org" target="_blank" rel="noopener noreferrer">https://example.org</a>`,
		`<a href="mailto:ced@example.org">ced@example.org</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("manca %q in\n%s", want, got)
		}
	}
}

func TestRenderRawHTMLOmitted(t *testing.T) {
	got := r("ciao <script>alert(1)</script> <b>x</b>\n\n<div onclick=x>y</div>")
	if strings.Contains(got, "<script") || strings.Contains(got, "<b>") || strings.Contains(got, "<div") {
		t.Fatalf("HTML grezzo passato: %s", got)
	}
}

var dangerousAttr = regexp.MustCompile(`(?i)(href|src)="\s*(javascript|vbscript|data|file):`)

func TestRenderDropsDangerousURLs(t *testing.T) {
	for _, src := range []string{
		"[x](javascript:alert(1))",
		"[x](JavaScript:alert(1))",
		"[x]( javascript:alert(1))",
		"[x](java&#x73;cript:alert(1))",
		"[x](vbscript:msgbox)",
		"[x](data:text/html;base64,PHNjcmlwdD4=)",
		"<javascript:alert(1)>",
	} {
		if got := r(src); dangerousAttr.MatchString(got) {
			t.Errorf("%q → %s", src, got)
		}
	}
	for _, src := range []string{
		"![i](javascript:alert(1))",
		"![i](data:image/svg+xml;base64,PHN2Zz4=)",
		"![i](mailto:a@b.c)",
		"![i](//evil.example/x.png)",
	} {
		if got := r(src); strings.Contains(got, "<img") {
			t.Errorf("immagine %q non scartata: %s", src, got)
		}
	}
	if got := r("[scrivi](mailto:ced@example.org)"); !strings.Contains(got, `href="mailto:ced@example.org"`) {
		t.Errorf("mailto nei link deve passare: %s", got)
	}
	if got := r("[testo del link](javascript:alert(1))"); !strings.Contains(got, "testo del link") || strings.Contains(got, "<a") {
		t.Errorf("link scartato: deve restare solo il testo: %s", got)
	}
}

func TestRenderHeadingsShifted(t *testing.T) {
	got := r("# Uno\n\n## Due\n\n###### Sei")
	for _, want := range []string{"<h2", "<h3", "<h6"} {
		if !strings.Contains(got, want) {
			t.Errorf("manca %s: %s", want, got)
		}
	}
	if strings.Contains(got, "<h1") {
		t.Errorf("h1 non ammesso: %s", got)
	}
}

func TestRenderTablesAndLists(t *testing.T) {
	got := r("| a | b |\n|---|---|\n| 1 | 2 |\n\n- uno\n- due\n\n~~via~~")
	for _, want := range []string{"<table>", "<td>1</td>", "<ul>", "<del>via</del>"} {
		if !strings.Contains(got, want) {
			t.Errorf("manca %s: %s", want, got)
		}
	}
}

func TestRenderRelativeRewrite(t *testing.T) {
	opt := Options{
		LinkBase:  "https://github.com/o/r/blob/main/docs/",
		ImageBase: "https://raw.githubusercontent.com/o/r/main/docs/",
		LinkRoot:  "https://github.com/o/r/blob/main/",
		ImageRoot: "https://raw.githubusercontent.com/o/r/main/",
	}
	got := string(Render("[altro](altro.md) ![s](img/s.png) [ass](https://x.org/a) [ancora](#sez) ![u](/img/a.png) [qui](./b.md) [radice](/README.md)", opt))
	for _, want := range []string{
		`href="https://github.com/o/r/blob/main/docs/altro.md"`,
		`src="https://raw.githubusercontent.com/o/r/main/docs/img/s.png"`,
		`href="https://x.org/a"`,
		`href="#sez"`,
		`src="https://raw.githubusercontent.com/o/r/main/img/a.png"`, // "/" = radice del repository, come su GitHub
		`href="https://github.com/o/r/blob/main/docs/b.md"`,
		`href="https://github.com/o/r/blob/main/README.md"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("manca %s: %s", want, got)
		}
	}
	if got := r("![u](/uploads/guide/a.png)"); !strings.Contains(got, `src="/uploads/guide/a.png"`) {
		t.Errorf("relativo interno: %s", got)
	}
}

// Le entità nell'URL si decodificano prima del controllo: &#x2F;&#x2F;host è
// //host (host esterno senza schema), anche se codificato due volte.
func TestRenderLinkEntities(t *testing.T) {
	for _, src := range []string{
		"[x](&#x2F;&#x2F;evil.example/a)",
		"[x](&#47;/evil.example/a)",
		"[x](&amp;#x2F;&amp;#x2F;evil.example/a)",
		"[x](&#x2F;&#x2F;evil.example/a \"t\")",
	} {
		got := r(src)
		if strings.Contains(got, "<a") || !strings.Contains(got, "x") {
			t.Errorf("%s → %s", src, got)
		}
	}
	if got := r("[q](https://example.it/?a=1&amp;b=2)"); !strings.Contains(got, `href="https://example.it/?a=1&amp;b=2"`) {
		t.Errorf("entità lecita: %s", got)
	}
}

func TestPlain(t *testing.T) {
	src := "# Titolo\n\nTesto **forte** e [link](https://x.org) con ![alt immagine](a.png).\n\n- uno\n- due"
	if got := Plain(src, 0); got != "Titolo Testo forte e link con alt immagine. uno due" {
		t.Fatalf("Plain: %q", got)
	}
	if got := Plain("àèìòù parola lunghissima", 12); got != "àèìòù…" {
		t.Fatalf("troncamento: %q", got)
	}
	if got := Plain("breve", 200); got != "breve" {
		t.Fatalf("corto: %q", got)
	}
	if got := Plain("riga\nsotto", 0); got != "riga sotto" {
		t.Fatalf("a capo: %q", got)
	}
}

func TestHasRich(t *testing.T) {
	if HasRich("solo testo **forte**") {
		t.Error("testo semplice non è ricco")
	}
	if !HasRich("![a](b.png)") || !HasRich("| a |\n|---|\n| 1 |") {
		t.Error("immagini e tabelle sono ricche")
	}
	if !HasRich("vedi https://x.org") || !HasRich("[a](https://x.org)") || !HasRich("scrivi a a@b.it") {
		t.Error("i link (anche nudi) si perdono nell'estratto")
	}
}

func FuzzRender(f *testing.F) {
	for _, s := range []string{"# t", "[a](javascript:x)", "<script>", "![i](data:x)", "| a |\n|-|\n| b |", "[a](b.md)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		got := string(Render(src, Options{LinkBase: "https://github.com/o/r/blob/m/", ImageBase: "https://raw.githubusercontent.com/o/r/m/"}))
		if strings.Contains(strings.ToLower(got), "<script") || dangerousAttr.MatchString(got) {
			t.Fatalf("%q → %s", src, got)
		}
		Plain(src, 50)
		HasRich(src)
	})
}
