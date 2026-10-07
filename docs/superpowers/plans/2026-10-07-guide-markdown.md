# Guide ricche e avvisi in Markdown — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** guide di tipo link/Markdown/PDF/GitHub, avvisi in Markdown, editor visuale TipTap nell'admin con immagini caricate.

**Architecture:** un renderer Go unico (`internal/markdown`, goldmark + sanitizzazione AST) usato da plancia, anteprima admin, guide GitHub e notifiche; contenuti GitHub scaricati da `internal/guidesrc` e tenuti in cache nel DB; upload a pezzi generico in `internal/web/media.go`; editor TipTap impacchettato con esbuild in uno stage Node del Dockerfile (Node solo in container).

**Tech Stack:** Go 1.26, `github.com/yuin/goldmark` v1.8.x, SQLite modernc, HTMX 2; TipTap 3.31 (`@tiptap/core`, `@tiptap/starter-kit`, `@tiptap/markdown`, `@tiptap/extension-image`, `@tiptap/extension-table`, `@tiptap/extensions`), esbuild 0.28, pnpm 12, `node:24-alpine`.

**Spec:** `docs/superpowers/specs/2026-10-07-guide-markdown-design.md`

## Global Constraints

- CSP invariata: `default-src 'self'; img-src 'self' https: data:; style-src 'self'; style-src-attr 'unsafe-inline'; script-src 'self'; …` — niente `<script>`/`<style>` inline, niente `on*=`.
- Il reverse proxy sostituisce 4xx/5xx: pagine pubbliche "non disponibile" con **200**; endpoint JSON dell'editor (`/admin/media*`, `/admin/anteprima`) sempre **200** con `{"ok":true|false,…}`; messaggi d'errore nel browser generici (mai il corpo della risposta).
- Validazioni dei form admin: 422 con la sezione (come oggi).
- Upload a pezzi da **512 KB**; immagini PNG/JPEG/WebP max **2 MB**; PDF max **20 MB**; tipo sempre dal contenuto.
- GitHub: solo `https://github.com/{owner}/{repo}/blob/{ref}/{path}.md`, download max **1 MB**, timeout **15 s**, redirect solo verso `raw.githubusercontent.com` (max 3), `GUIDE_REFRESH_HOURS` predefinito **6** (`0` = solo manuale).
- Avvisi: corpo max **20000** caratteri (oggi 2000); estratto carosello **200** rune; testo notifiche **120** rune.
- Guide `markdown`: corpo max **100000** caratteri.
- Migrazioni append-only: la nuova è **v7**, mai toccare v1–v6.
- Nuova env var in tre posti: `docker-compose.yml`, `.env.example`, `internal/config`.
- Node mai richiesto sul PC: build dell'editor solo via Docker (`scripts/editor.sh`, stage `editor` del Dockerfile). `web/static/vendor/` in `.gitignore`.
- Nessun ente/host scritto nel codice o nei template.
- Commit message in italiano come la storia del repo, terminare con `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Avvisi esistenti in testo semplice** (righe singole, URL nudi, email) devono apparire come prima: hard wraps + linkify GFM. Test in Task 2 (`TestRenderLegacyPlainText`).
2. **Immagine caricata nell'editor ma form non ancora salvato** non va cancellata dalla pulizia degli orfani: soglia 24 h. Test in Task 4 (`TestCleanMediaKeepsRecentAndReferenced`).
3. **GitHub irraggiungibile durante il refresh** non deve svuotare la guida: resta l'ultima copia. Test in Task 9 (`TestRefreshKeepsBodyOnError`).
4. **Guida di un'app disabilitata o senza URL** non deve aprirsi da `/guide/{id}` (stessa regola della plancia). Test in Task 8 (`TestGuidePageVisibility`).
5. **Markdown con link `JavaScript:` in maiuscolo / con spazi o entità** non deve produrre href eseguibili. Test e fuzz in Task 2 (`TestRenderDropsDangerousURLs`, `FuzzRender`).

## File Structure

```
internal/markdown/markdown.go        Render, Plain, HasRich, sanitizzazione AST
internal/markdown/markdown_test.go   test + fuzz
internal/guidesrc/github.go          ParseGitHubURL, Source
internal/guidesrc/fetch.go           Fetcher (client HTTP limitato)
internal/guidesrc/*_test.go
internal/database/migrations.go      + migrateV7Guides
internal/database/guides.go          campi nuovi, kind, query refresh/plancia
internal/database/media.go           MediaReferenced
internal/web/media.go                upload a pezzi generico, servizio /uploads/guide, pulizia orfani
internal/web/admin_preview.go        POST /admin/anteprima
internal/web/guides_public.go        /guide/{id}, /guide/{id}/pdf
internal/web/avviso.go               /avvisi/{id}
internal/web/guide_refresh.go        job di refresh GitHub + "Aggiorna ora"
internal/web/admin_guides.go         form per tipo
internal/web/render.go               funzioni template md, excerpt, rich, guideHref
internal/notify/push.go              Payload con markdown.Plain e url /avvisi/{id}
internal/config/config.go            GuideRefreshHours
web/templates/guida.html, avviso.html (nuove), admin_guide.html, admin_avvisi.html,
  partials_dashboard.html, avvisi.html, admin_base.html
web/static/js/admin.js               tipo guida, upload PDF, anteprima
web/editor/{package.json,pnpm-lock.yaml,build.mjs,src/editor.js,src/editor.css}
scripts/editor.sh
Dockerfile, .gitignore, .github/dependabot.yml, .github/workflows/test.yml
```

---

### Task 1: Prova — viewer PDF di Chrome/Edge con CSP sandbox

Esito usato in Task 8. Codice usa-e-getta, non committato.

**Files:**
- Create (scratchpad, non nel repo): `pdfcsp/main.go`
- Modify: `docs/superpowers/specs/2026-10-07-guide-markdown-design.md` (sezione 6, esito)

- [ ] **Step 1: Server di prova**

```go
// pdfcsp/main.go — go run . <file.pdf>
package main

import (
	"net/http"
	"os"
)

func main() {
	pdf := os.Args[1]
	serve := func(csp string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Disposition", `inline; filename="prova.pdf"`)
			if csp != "" {
				w.Header().Set("Content-Security-Policy", csp)
			}
			http.ServeFile(w, r, pdf)
		}
	}
	http.HandleFunc("/sandbox.pdf", serve("sandbox; default-src 'none'; style-src 'unsafe-inline'"))
	http.HandleFunc("/object.pdf", serve("default-src 'none'; object-src 'self'; frame-ancestors 'none'"))
	http.HandleFunc("/nocsp.pdf", serve(""))
	http.ListenAndServe("127.0.0.1:18181", nil)
}
```

Generare un PDF di prova con Chrome headless (`chrome --headless --print-to-pdf=prova.pdf about:blank`) o usarne uno qualsiasi.

- [ ] **Step 2: Aprire i tre URL in Chrome e in Edge (Playwright `browser_navigate` + screenshot, o a mano)**

Atteso: annotare per ciascuno se il PDF viene visualizzato inline, scaricato o mostra pagina bianca.

- [ ] **Step 3: Annotare l'esito nella spec e scegliere la CSP**

Regola: usare `sandbox; default-src 'none'; style-src 'unsafe-inline'` se il viewer funziona in entrambi; altrimenti `default-src 'none'; object-src 'self'; frame-ancestors 'none'`. Scrivere la CSP scelta in sezione 6 della spec al posto di "Da verificare…".

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-10-07-guide-markdown-design.md
git commit -m "Spec: CSP del PDF verificata nel viewer di Chrome ed Edge"
```

---

### Task 2: Renderer Markdown — `internal/markdown`

**Files:**
- Create: `internal/markdown/markdown.go`, `internal/markdown/markdown_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces:
  - `type Options struct { LinkBase, ImageBase string }`
  - `func Render(src string, opt Options) template.HTML`
  - `func Plain(src string, n int) string` (n ≤ 0 = nessun troncamento)
  - `func HasRich(src string) bool` — true se contiene immagini o tabelle

- [ ] **Step 1: Aggiungere goldmark**

Run: `go get github.com/yuin/goldmark@v1.8.6`

- [ ] **Step 2: Test che falliscono**

```go
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

func TestRenderDropsDangerousURLs(t *testing.T) {
	for _, src := range []string{
		"[x](javascript:alert(1))",
		"[x](JavaScript:alert(1))",
		"[x]( javascript:alert(1))",
		"[x](java&#x73;cript:alert(1))",
		"[x](vbscript:msgbox)",
		"[x](data:text/html;base64,PHNjcmlwdD4=)",
		"![i](javascript:alert(1))",
		"![i](data:image/svg+xml;base64,PHN2Zz4=)",
		"![i](mailto:a@b.c)",
		"<javascript:alert(1)>",
	} {
		got := r(src)
		if dangerousAttr.MatchString(got) || strings.Contains(got, "<img") && strings.Contains(src, "!") {
			t.Errorf("%q → %s", src, got)
		}
	}
	if got := r("[scrivi](mailto:ced@example.org)"); !strings.Contains(got, `href="mailto:ced@example.org"`) {
		t.Errorf("mailto nei link deve passare: %s", got)
	}
	if got := r("[x](javascript:alert(1))"); !strings.Contains(got, "x") {
		t.Errorf("il testo del link scartato deve restare: %s", got)
	}
}

var dangerousAttr = regexp.MustCompile(`(?i)(href|src)="\s*(javascript|vbscript|data|file):`)

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
	}
	got := string(Render("[altro](altro.md) ![s](img/s.png) [ass](https://x.org/a) [ancora](#sez) ![u](/uploads/guide/a.png)", opt))
	for _, want := range []string{
		`href="https://github.com/o/r/blob/main/docs/altro.md"`,
		`src="https://raw.githubusercontent.com/o/r/main/docs/img/s.png"`,
		`href="https://x.org/a"`,
		`href="#sez"`,
		`src="/uploads/guide/a.png"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("manca %s: %s", want, got)
		}
	}
	// Senza basi i relativi restano come sono.
	if got := r("![u](/uploads/guide/a.png)"); !strings.Contains(got, `src="/uploads/guide/a.png"`) {
		t.Errorf("relativo interno: %s", got)
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
}

func TestHasRich(t *testing.T) {
	if HasRich("solo testo **forte**") {
		t.Error("testo semplice non è ricco")
	}
	if !HasRich("![a](b.png)") || !HasRich("| a |\n|---|\n| 1 |") {
		t.Error("immagini e tabelle sono ricche")
	}
}

func FuzzRender(f *testing.F) {
	for _, s := range []string{"# t", "[a](javascript:x)", "<script>", "![i](data:x)", "| a |\n|-|\n| b |"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		got := string(Render(src, Options{LinkBase: "https://github.com/o/r/blob/m/", ImageBase: "https://raw.githubusercontent.com/o/r/m/"}))
		if strings.Contains(strings.ToLower(got), "<script") || dangerousAttr.MatchString(got) {
			t.Fatalf("%q → %s", src, got)
		}
		Plain(src, 50)
	})
}
```

- [ ] **Step 3: Verificare il fallimento**

Run: `go test ./internal/markdown/`
Expected: FAIL (`undefined: Render`).

- [ ] **Step 4: Implementazione**

```go
// Package markdown rende il Markdown di avvisi e guide in HTML sicuro.
// HTML grezzo disattivato; URL ammessi: http, https, mailto (solo link), relativi.
package markdown

import (
	"bytes"
	"html/template"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Options: basi per riscrivere link e immagini relativi (guide GitHub).
// Vuote = relativi lasciati come sono.
type Options struct {
	LinkBase, ImageBase string
}

func newMD(opt Options) goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithASTTransformers(
			util.Prioritized(sanitizer{opt}, 100),
		)),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)
}

// Render: errore di rendering (non atteso) → testo escapato.
func Render(src string, opt Options) template.HTML {
	var b bytes.Buffer
	if err := newMD(opt).Convert([]byte(src), &b); err != nil {
		return template.HTML("<p>" + template.HTMLEscapeString(src) + "</p>") //nolint:gosec // escapato
	}
	return template.HTML(b.String()) //nolint:gosec // HTML grezzo disattivato, URL filtrati
}

type sanitizer struct{ opt Options }

func (s sanitizer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	var drop, unwrap []ast.Node
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			n.Level = min(n.Level+1, 6)
		case *ast.Link:
			dest, ok := s.fix(string(n.Destination), s.opt.LinkBase, true)
			if !ok {
				unwrap = append(unwrap, n)
				return ast.WalkContinue, nil
			}
			n.Destination = []byte(dest)
			external(n, dest)
		case *ast.AutoLink:
			u := string(n.URL(src))
			if n.AutoLinkType == ast.AutoLinkEmail {
				return ast.WalkContinue, nil
			}
			if _, ok := s.fix(u, "", true); !ok {
				drop = append(drop, n)
				return ast.WalkContinue, nil
			}
			external(n, u)
		case *ast.Image:
			dest, ok := s.fix(string(n.Destination), s.opt.ImageBase, false)
			if !ok {
				drop = append(drop, n)
				return ast.WalkSkipChildren, nil
			}
			n.Destination = []byte(dest)
		}
		return ast.WalkContinue, nil
	})
	for _, n := range unwrap { // link scartato: resta il testo
		p := n.Parent()
		for c := n.FirstChild(); c != nil; {
			next := c.NextSibling()
			p.InsertBefore(p, n, c)
			c = next
		}
		p.RemoveChild(p, n)
	}
	for _, n := range drop {
		n.Parent().RemoveChild(n.Parent(), n)
	}
}

func external(n ast.Node, dest string) {
	if strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://") {
		n.SetAttributeString("target", []byte("_blank"))
		n.SetAttributeString("rel", []byte("noopener noreferrer"))
	}
}

// fix valida un URL e riscrive i relativi con base. link=false → immagini:
// niente mailto.
func (s sanitizer) fix(raw, base string, link bool) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.ContainsAny(raw, "\x00\t\r\n") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return raw, u.Host != ""
	case "mailto":
		return raw, link
	case "":
		if u.Host != "" || strings.HasPrefix(raw, "//") {
			return "", false // //host/x: host esterno senza schema
		}
		if base == "" || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "#") || raw == "" {
			return raw, true
		}
		return base + strings.TrimPrefix(raw, "./"), true
	}
	return "", false
}

// Plain: testo senza sintassi Markdown, spazi compattati, troncato a n rune
// (n ≤ 0 = intero) sull'ultimo spazio, con "…".
func Plain(src string, n int) string {
	b := []byte(src)
	doc := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(b))
	var out strings.Builder
	ast.Walk(doc, func(nd ast.Node, entering bool) (ast.WalkStatus, error) {
		switch nd := nd.(type) {
		case *ast.Text:
			if entering {
				out.Write(nd.Segment.Value(b))
				if nd.SoftLineBreak() || nd.HardLineBreak() {
					out.WriteByte(' ')
				}
			}
		case *ast.String:
			if entering {
				out.Write(nd.Value)
			}
		case *ast.AutoLink:
			if entering {
				out.Write(nd.Label(b))
			}
		default:
			if !entering && nd.Type() == ast.TypeBlock {
				out.WriteByte(' ')
			}
		}
		return ast.WalkContinue, nil
	})
	s := strings.Join(strings.Fields(out.String()), " ")
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	cut := string([]rune(s)[:n-1])
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

// HasRich: il contenuto ha immagini o tabelle (in carosello serve "Leggi tutto").
func HasRich(src string) bool {
	doc := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader([]byte(src)))
	rich := false
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && (n.Kind() == ast.KindImage || n.Kind() == extast.KindTable) {
			rich = true
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return rich
}
```

Note per l'implementatore:
- `TestPlain` atteso `"Titolo Testo forte e link con alt immagine. uno due"`: il punto dopo l'immagine è un nodo testo adiacente, quindi `immagine.` senza spazio. Se l'output differisce solo per spazi attorno alla punteggiatura, correggere il walker (non il test).
- `Plain("àèìòù parola lunghissima", 12)`: 11 rune tagliate = `àèìòù parol` → ultimo spazio → `àèìòù` + `…`.
- Se goldmark non riconosce `[x]( javascript:…)` come link (spazio iniziale), il testo resta letterale: va bene, il test controlla solo l'assenza di href pericolosi.

- [ ] **Step 5: Verificare che passino (fuzz incluso per 30 s)**

Run: `go test ./internal/markdown/ && go test ./internal/markdown/ -run '^$' -fuzz FuzzRender -fuzztime 30s`
Expected: PASS, nessun crasher.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/markdown
git commit -m "Renderer Markdown sicuro per avvisi e guide (goldmark)"
```

---

### Task 3: Dati — migrazione v7 e guide per tipo

**Files:**
- Modify: `internal/database/migrations.go`, `internal/database/guides.go`
- Create: `internal/database/media.go`
- Test: `internal/database/guides_test.go` (aggiungere), `internal/database/media_test.go`

**Interfaces:**
- Produces:
  - costanti `GuideKindLink = "link"`, `GuideKindMarkdown = "markdown"`, `GuideKindPDF = "pdf"`, `GuideKindGitHub = "github"`; `func ValidGuideKind(k string) bool`
  - `Guide` con nuovi campi `File string`, `SourceURL string`, `FetchedAt *time.Time`, `FetchError string`
  - `func (db *DB) GetPlanciaGuide(id int64) (Guide, error)` — solo guide abilitate, generali o di un'app visibile (`enabled=1 AND url<>''`); altrimenti `ErrNotFound`
  - `func (db *DB) GuidesToRefresh(before time.Time) ([]Guide, error)` — `kind='github'`, `enabled=1`, `fetched_at IS NULL OR fetched_at < before`
  - `func (db *DB) SetGuideFetched(id int64, body string, at time.Time) error` (azzera `fetch_error`)
  - `func (db *DB) SetGuideFetchError(id int64, msg string) error`
  - `func (db *DB) MediaReferenced(name string) (bool, error)` — `name` presente in `alerts.body`, in `guides.body` di tipo `markdown` o in `guides.file`

- [ ] **Step 1: Test che falliscono**

```go
// internal/database/guides_test.go (aggiungere)
func TestMigrationV7GuideColumns(t *testing.T) {
	db := newTestDB(t)
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	id, err := db.CreateGuide(Guide{Title: "Manuale", Kind: GuideKindGitHub, SourceURL: "https://github.com/o/r/blob/main/a.md", Body: "# A", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetGuideFetched(id, "# B", at); err != nil {
		t.Fatal(err)
	}
	g, _ := db.GetGuide(id)
	if g.Body != "# B" || g.FetchedAt == nil || !g.FetchedAt.Equal(at) || g.FetchError != "" || g.SourceURL == "" {
		t.Fatalf("dopo fetch: %+v", g)
	}
	if err := db.SetGuideFetchError(id, "404"); err != nil {
		t.Fatal(err)
	}
	g, _ = db.GetGuide(id)
	if g.Body != "# B" || g.FetchError != "404" {
		t.Fatalf("errore deve lasciare il body: %+v", g)
	}
	todo, _ := db.GuidesToRefresh(at.Add(time.Hour))
	if len(todo) != 1 || todo[0].ID != id {
		t.Fatalf("da aggiornare: %+v", todo)
	}
	if todo, _ = db.GuidesToRefresh(at); len(todo) != 0 {
		t.Fatalf("appena aggiornata: %+v", todo)
	}
}

func TestGetPlanciaGuide(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps() // seed: Rubrica e Webmail senza URL → non visibili
	hidden := apps[0].ID
	gen, _ := db.CreateGuide(Guide{Title: "G", Kind: GuideKindMarkdown, Body: "x", Enabled: true})
	off, _ := db.CreateGuide(Guide{Title: "Off", Kind: GuideKindMarkdown, Body: "x"})
	ofHidden, _ := db.CreateGuide(Guide{AppID: &hidden, Title: "H", Kind: GuideKindMarkdown, Body: "x", Enabled: true})
	if _, err := db.GetPlanciaGuide(gen); err != nil {
		t.Fatalf("generale abilitata: %v", err)
	}
	for _, id := range []int64{off, ofHidden, 9999} {
		if _, err := db.GetPlanciaGuide(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("guida %d: atteso ErrNotFound, %v", id, err)
		}
	}
}

func TestValidGuideKind(t *testing.T) {
	for _, k := range []string{"link", "markdown", "pdf", "github"} {
		if !ValidGuideKind(k) {
			t.Error(k)
		}
	}
	if ValidGuideKind("html") || ValidGuideKind("") {
		t.Error("tipo non valido accettato")
	}
}
```

```go
// internal/database/media_test.go
package database

import "testing"

func TestMediaReferenced(t *testing.T) {
	db := newTestDB(t)
	img := "0123456789abcdef0123456789abcdef.png"
	pdf := "fedcba9876543210fedcba9876543210.pdf"
	db.CreateAlert(Alert{Title: "A", Body: "![x](/uploads/guide/" + img + ")", Level: "info", StartsAt: time.Now()})
	db.CreateGuide(Guide{Title: "P", Kind: GuideKindPDF, File: pdf, Enabled: true})
	for name, want := range map[string]bool{img: true, pdf: true, "00000000000000000000000000000000.png": false} {
		got, err := db.MediaReferenced(name)
		if err != nil || got != want {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
}
```

(adeguare `CreateAlert` ai campi obbligatori come negli altri test del pacchetto; aggiungere `import "time"`.)

- [ ] **Step 2: Verificare il fallimento**

Run: `go test ./internal/database/`
Expected: FAIL (campi/funzioni non definiti).

- [ ] **Step 3: Migrazione**

In `migrations.go` aggiungere `migrateV7Guides` in coda alla lista e:

```go
// migrateV7Guides: guide Markdown, PDF e GitHub (sotto-progetto 2).
func migrateV7Guides(tx *sql.Tx) error {
	for _, q := range []string{
		`ALTER TABLE guides ADD COLUMN file TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE guides ADD COLUMN source_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE guides ADD COLUMN fetched_at TEXT`,
		`ALTER TABLE guides ADD COLUMN fetch_error TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: `guides.go`**

```go
const (
	GuideKindLink     = "link"     // link esterno
	GuideKindMarkdown = "markdown" // testo scritto nell'admin
	GuideKindPDF      = "pdf"      // PDF caricato (File)
	GuideKindGitHub   = "github"   // file .md di GitHub, copia in Body
)

func ValidGuideKind(k string) bool {
	switch k {
	case GuideKindLink, GuideKindMarkdown, GuideKindPDF, GuideKindGitHub:
		return true
	}
	return false
}

type Guide struct {
	ID         int64
	AppID      *int64
	Title      string
	Kind       string
	URL        string
	Body       string
	File       string     // PDF in UPLOAD_DIR/guide
	SourceURL  string     // URL GitHub inserito dall'admin
	FetchedAt  *time.Time // ultimo download riuscito (GitHub)
	FetchError string     // errore dell'ultimo tentativo, "" = ok
	SortOrder  int
	Enabled    bool
}

const guideCols = `g.id, g.app_id, g.title, g.kind, g.url, g.body, g.file, g.source_url, g.fetched_at, g.fetch_error, g.sort_order, g.enabled`

func scanGuide(s scanner) (Guide, error) {
	var g Guide
	var appID sql.NullInt64
	var fetched sql.NullString
	err := s.Scan(&g.ID, &appID, &g.Title, &g.Kind, &g.URL, &g.Body, &g.File, &g.SourceURL, &fetched, &g.FetchError, &g.SortOrder, &g.Enabled)
	if err != nil {
		return g, err
	}
	if appID.Valid {
		g.AppID = &appID.Int64
	}
	if fetched.Valid {
		t, err := parseTime(fetched.String)
		if err != nil {
			return g, err
		}
		g.FetchedAt = &t
	}
	return g, nil
}
```

`createGuide`/`updateGuide`: aggiungere `file` e `source_url` a colonne e argomenti (stessa posizione dopo `body`). `fetched_at`/`fetch_error` si scrivono solo con le funzioni dedicate.

```go
func (db *DB) GetPlanciaGuide(id int64) (Guide, error) {
	g, err := scanGuide(db.QueryRow(`SELECT `+guideCols+` FROM guides g
LEFT JOIN apps a ON a.id = g.app_id
WHERE g.id = ? AND g.enabled = 1 AND (g.app_id IS NULL OR (`+visibleApp+`))`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

func (db *DB) GuidesToRefresh(before time.Time) ([]Guide, error) {
	return queryGuides(db, `SELECT `+guideCols+` FROM guides g
WHERE g.kind = 'github' AND g.enabled = 1 AND (g.fetched_at IS NULL OR g.fetched_at < ?)
ORDER BY g.fetched_at IS NOT NULL, g.fetched_at, g.id`, formatTime(before))
}

func (db *DB) SetGuideFetched(id int64, body string, at time.Time) error {
	return checkAffected(db.Exec(`UPDATE guides SET body = ?, fetched_at = ?, fetch_error = '' WHERE id = ?`, body, formatTime(at), id))
}

func (db *DB) SetGuideFetchError(id int64, msg string) error {
	return checkAffected(db.Exec(`UPDATE guides SET fetch_error = ? WHERE id = ?`, msg, id))
}
```

(`visibleApp` è già definita in `dashboard.go`; aggiornare il commento in testa a `guides.go`.)

- [ ] **Step 5: `media.go`**

```go
package database

// MediaReferenced: il file caricato (nome casuale, senza percorso) è usato da
// un avviso, da una guida Markdown o come PDF di una guida.
func (db *DB) MediaReferenced(name string) (bool, error) {
	like := "%" + name + "%"
	var n int
	err := db.QueryRow(`SELECT
	(SELECT COUNT(*) FROM alerts WHERE body LIKE ?) +
	(SELECT COUNT(*) FROM guides WHERE (kind = 'markdown' AND body LIKE ?) OR file = ?)`,
		like, like, name).Scan(&n)
	return n > 0, err
}
```

(I nomi sono `[0-9a-f]{32}.ext`: niente `%`/`_` da escapare in `LIKE`.)

- [ ] **Step 6: Verificare**

Run: `go test ./internal/database/ && go build ./...`
Expected: PASS; build ok (i chiamanti in `web` compilano: i campi nuovi sono opzionali).

- [ ] **Step 7: Commit**

```bash
git add internal/database
git commit -m "Migrazione v7: guide Markdown, PDF e GitHub"
```

---

### Task 4: Upload a pezzi dei media e pulizia degli orfani

**Files:**
- Create: `internal/web/media.go`, `internal/web/media_test.go`
- Modify: `internal/web/server.go` (route, campo `media`), `internal/web/uploads.go` (costante `uploadGuide`, regex nomi)

**Interfaces:**
- Consumes: `db.MediaReferenced(name) (bool, error)` (Task 3)
- Produces:
  - route admin `POST /admin/media` (avvio; form `tipo=immagine|pdf`) → `{"ok":true,"id":"<hex>","chunk":524288}`; `POST /admin/media/{id}/pezzo?n=N` (corpo = byte) → `{"ok":true}`; `POST /admin/media/{id}/fine` → `{"ok":true,"name":"<file>","url":"/uploads/guide/<file>"}`; errori sempre `{"ok":false,"error":"…"}` con 200
  - route pubblica `GET /uploads/guide/{file}` (immagini; i PDF **non** si servono da qui)
  - `const uploadGuide = "guide"`
  - `func (s *Server) cleanMedia()` — cancella da `UPLOAD_DIR/guide` i file più vecchi di 24 h non referenziati
  - `func guideMediaRe` = `^[0-9a-f]{32}\.(png|jpg|webp|pdf)$`

- [ ] **Step 1: Test che falliscono**

```go
package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pngBytes: PNG minimo valido (8 byte di firma + IHDR) riconosciuto da DetectContentType.
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)

func mediaPost(t *testing.T, s *Server, c *http.Cookie, target, ctype string, body []byte) map[string]any {
	t.Helper()
	req := httptest.NewRequest("POST", target, bytes.NewReader(body))
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%s: status %d (deve essere sempre 200)", target, rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: JSON non valido: %s", target, rec.Body)
	}
	return out
}

func uploadMedia(t *testing.T, s *Server, c *http.Cookie, tipo string, data []byte) map[string]any {
	t.Helper()
	start := mediaPost(t, s, c, "/admin/media", "application/x-www-form-urlencoded", []byte("tipo="+tipo))
	if start["ok"] != true {
		t.Fatalf("avvio: %v", start)
	}
	id := start["id"].(string)
	chunk := int(start["chunk"].(float64))
	for n, off := 0, 0; off < len(data); n, off = n+1, off+chunk {
		end := min(off+chunk, len(data))
		r := mediaPost(t, s, c, "/admin/media/"+id+"/pezzo?n="+itoa(int64(n)), "application/octet-stream", data[off:end])
		if r["ok"] != true {
			return r
		}
	}
	return mediaPost(t, s, c, "/admin/media/"+id+"/fine", "application/x-www-form-urlencoded", nil)
}

func TestMediaUploadImage(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	big := append(append([]byte{}, pngBytes...), make([]byte, 700<<10)...) // > 1 pezzo
	out := uploadMedia(t, s, c, "immagine", big)
	if out["ok"] != true || !strings.HasPrefix(out["url"].(string), "/uploads/guide/") || !strings.HasSuffix(out["url"].(string), ".png") {
		t.Fatalf("upload: %v", out)
	}
	rec := do(t, s, "GET", out["url"].(string), nil, nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") || rec.Body.Len() != len(big) {
		t.Fatalf("servizio: %d %q %d", rec.Code, rec.Header().Get("Content-Security-Policy"), rec.Body.Len())
	}
}

func TestMediaUploadRejects(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	if out := uploadMedia(t, s, c, "immagine", svg); out["ok"] != false {
		t.Errorf("SVG come immagine: %v", out)
	}
	if out := uploadMedia(t, s, c, "immagine", append(append([]byte{}, pngBytes...), make([]byte, 2<<20)...)); out["ok"] != false {
		t.Errorf("immagine > 2 MB: %v", out)
	}
	if out := uploadMedia(t, s, c, "pdf", pngBytes); out["ok"] != false {
		t.Errorf("PNG come PDF: %v", out)
	}
	if out := uploadMedia(t, s, c, "pdf", []byte("%PDF-1.7\n%%EOF")); out["ok"] != true || !strings.HasSuffix(out["name"].(string), ".pdf") {
		t.Errorf("PDF valido: %v", out)
	}
	// Un PDF non si serve da /uploads/guide (solo da /guide/{id}/pdf, con la visibilità).
	out := uploadMedia(t, s, c, "pdf", []byte("%PDF-1.7\n%%EOF"))
	if rec := do(t, s, "GET", "/uploads/guide/"+out["name"].(string), nil, nil, nil); rec.Code == 200 {
		t.Error("PDF servito da /uploads/guide")
	}
	if out := mediaPost(t, s, c, "/admin/media/nonesiste/pezzo?n=0", "application/octet-stream", []byte("x")); out["ok"] != false {
		t.Errorf("upload inesistente: %v", out)
	}
}

func TestMediaRequiresAdmin(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if rec := do(t, s, "POST", "/admin/media", nil, nil, map[string]string{"Sec-Fetch-Site": "same-origin"}); rec.Code == 200 && strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatal("upload senza login")
	}
}

func TestCleanMediaKeepsRecentAndReferenced(t *testing.T) {
	s, db := newTestServer(t, nil)
	dir := s.uploadDir(uploadGuide)
	os.MkdirAll(dir, 0o750)
	old := fixedNow.Add(-25 * time.Hour)
	mk := func(name string, mod time.Time) {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("x"), 0o640)
		os.Chtimes(p, mod, mod)
	}
	orphan := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"
	recent := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.png"
	used := "cccccccccccccccccccccccccccccccc.png"
	mk(orphan, old)
	mk(recent, fixedNow.Add(-time.Hour))
	mk(used, old)
	db.CreateGuide(database.Guide{Title: "G", Kind: database.GuideKindMarkdown, Body: "![x](/uploads/guide/" + used + ")", Enabled: true})
	s.cleanMedia()
	for name, want := range map[string]bool{orphan: false, recent: true, used: true} {
		_, err := os.Stat(filepath.Join(dir, name))
		if (err == nil) != want {
			t.Errorf("%s: presente=%v, atteso %v", name, err == nil, want)
		}
	}
}
```

(aggiungere import `database`.)

- [ ] **Step 2: Verificare il fallimento**

Run: `go test ./internal/web/ -run 'Media'`
Expected: FAIL (route assenti, `cleanMedia` non definita).

- [ ] **Step 3: Implementazione `media.go`**

```go
package web

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const (
	uploadGuide    = "guide" // immagini e PDF di avvisi e guide
	mediaChunk     = 512 << 10
	maxImageBytes  = 2 << 20
	maxPDFBytes    = 20 << 20
	maxMediaUploads = 8
	mediaUploadTTL = time.Hour
	mediaOrphanAge = 24 * time.Hour
)

var guideMediaRe = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg|webp|pdf)$`)
var guideImageRe = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg|webp)$`)

// mediaUpload: un caricamento a pezzi in corso (file temporaneo in UPLOAD_DIR/.tmp).
type mediaUpload struct {
	path    string
	pdf     bool
	next    int
	size    int64
	started time.Time
}

type mediaUploads struct {
	mu   sync.Mutex
	byID map[string]*mediaUpload
}

func mediaJSON(w http.ResponseWriter, v map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func mediaFail(w http.ResponseWriter, msg string) { mediaJSON(w, map[string]any{"ok": false, "error": msg}) }

func (s *Server) handleMediaStart(w http.ResponseWriter, r *http.Request) {
	pdf := r.FormValue("tipo") == "pdf"
	s.media.mu.Lock()
	defer s.media.mu.Unlock()
	now := s.now()
	for id, u := range s.media.byID { // scaduti: file abbandonati
		if now.Sub(u.started) > mediaUploadTTL {
			os.Remove(u.path)
			delete(s.media.byID, id)
		}
	}
	if len(s.media.byID) >= maxMediaUploads {
		mediaFail(w, "Troppi caricamenti in corso, riprova fra poco.")
		return
	}
	b := make([]byte, 16)
	rand.Read(b)
	id := hex.EncodeToString(b)
	dir := filepath.Join(s.cfg.UploadDir, ".tmp")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		slog.Error("media: cartella temporanea", "err", err)
		mediaFail(w, "Caricamento non riuscito.")
		return
	}
	s.media.byID[id] = &mediaUpload{path: filepath.Join(dir, id), pdf: pdf, started: now}
	mediaJSON(w, map[string]any{"ok": true, "id": id, "chunk": mediaChunk})
}

func (s *Server) handleMediaChunk(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	s.media.mu.Lock()
	defer s.media.mu.Unlock()
	u := s.media.byID[r.PathValue("id")]
	switch {
	case u == nil:
		mediaFail(w, "Caricamento scaduto, riprova.")
		return
	case err != nil || n != u.next:
		mediaFail(w, "Caricamento interrotto, riprova.")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, mediaChunk+1))
	limit := int64(maxImageBytes)
	if u.pdf {
		limit = maxPDFBytes
	}
	if err != nil || len(data) > mediaChunk || u.size+int64(len(data)) > limit {
		mediaFail(w, tooBigMsg(u.pdf))
		return
	}
	f, err := os.OpenFile(u.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err == nil {
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		slog.Error("media: scrittura pezzo", "err", err)
		mediaFail(w, "Caricamento non riuscito.")
		return
	}
	u.next++
	u.size += int64(len(data))
	mediaJSON(w, map[string]any{"ok": true})
}

func tooBigMsg(pdf bool) string {
	if pdf {
		return "File troppo grande (massimo 20 MB)."
	}
	return "Immagine troppo grande (massimo 2 MB)."
}

func (s *Server) handleMediaFinish(w http.ResponseWriter, r *http.Request) {
	s.media.mu.Lock()
	id := r.PathValue("id")
	u := s.media.byID[id]
	delete(s.media.byID, id)
	s.media.mu.Unlock()
	if u == nil {
		mediaFail(w, "Caricamento scaduto, riprova.")
		return
	}
	defer os.Remove(u.path)
	head := make([]byte, 512)
	f, err := os.Open(u.path)
	if err != nil {
		mediaFail(w, "Caricamento non riuscito.")
		return
	}
	k, _ := io.ReadFull(f, head)
	f.Close()
	ext, ok := mediaExt(head[:k], u.pdf)
	if !ok {
		if u.pdf {
			mediaFail(w, "Il file non è un PDF.")
		} else {
			mediaFail(w, "Formato non ammesso: usa PNG, JPEG o WebP.")
		}
		return
	}
	b := make([]byte, 16)
	rand.Read(b)
	name := hex.EncodeToString(b) + "." + ext
	dir := s.uploadDir(uploadGuide)
	if err := os.MkdirAll(dir, 0o750); err == nil {
		err = os.Rename(u.path, filepath.Join(dir, name))
	}
	if err != nil {
		slog.Error("media: salvataggio", "err", err)
		mediaFail(w, "Caricamento non riuscito.")
		return
	}
	mediaJSON(w, map[string]any{"ok": true, "name": name, "url": "/uploads/guide/" + name})
}

// mediaExt: tipo dal contenuto, mai dal nome dichiarato.
func mediaExt(head []byte, pdf bool) (string, bool) {
	if pdf {
		return "pdf", bytes.HasPrefix(head, []byte("%PDF-"))
	}
	switch http.DetectContentType(head) {
	case "image/png":
		return "png", true
	case "image/jpeg":
		return "jpg", true
	case "image/webp":
		return "webp", true
	}
	return "", false
}

// handleGuideImage: solo immagini, in sandbox come le icone. I PDF passano da
// /guide/{id}/pdf, che controlla la visibilità della guida.
func (s *Server) handleGuideImage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !guideImageRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.uploadDir(uploadGuide), name))
}

// cleanMedia cancella i file non più usati da avvisi e guide. Solo quelli più
// vecchi di 24 ore: un'immagine appena caricata in un form non ancora salvato
// non è ancora referenziata.
func (s *Server) cleanMedia() {
	dir := s.uploadDir(uploadGuide)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		info, err := e.Info()
		if err != nil || !guideMediaRe.MatchString(name) || s.now().Sub(info.ModTime()) < mediaOrphanAge {
			continue
		}
		used, err := s.db.MediaReferenced(name)
		if err != nil {
			slog.Warn("media: verifica riferimenti", "err", err)
			return
		}
		if !used {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.Warn("media: rimozione orfano", "file", name, "err", err)
			}
		}
	}
}
```

In `server.go`: campo `media mediaUploads` inizializzato in `New` con `byID: map[string]*mediaUpload{}`; route:

```go
s.mux.HandleFunc("GET /uploads/guide/{file}", s.handleGuideImage)
s.mux.HandleFunc("POST /admin/media", s.requireAdmin(s.handleMediaStart))
s.mux.HandleFunc("POST /admin/media/{id}/pezzo", s.requireAdmin(s.handleMediaChunk))
s.mux.HandleFunc("POST /admin/media/{id}/fine", s.requireAdmin(s.handleMediaFinish))
```

Nota: `requireAdmin` senza sessione risponde 303/401+HX-Redirect: va bene (il test controlla solo che non ci sia `"ok":true`). `UPLOAD_DIR/.tmp` è dentro `uploads/`: verificare in `internal/backup` che la raccolta di `uploads/` salti le directory che iniziano con `.` (se no, aggiungere lo skip con un test in `internal/backup`).

- [ ] **Step 4: Verificare**

Run: `go test ./internal/web/ -run 'Media' && go test ./internal/backup/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web internal/backup
git commit -m "Upload a pezzi di immagini e PDF per avvisi e guide"
```

---

### Task 5: Avvisi in Markdown — carosello, pagina dell'avviso, notifiche

**Files:**
- Modify: `internal/web/render.go` (funzioni template), `internal/web/dashboard.go`, `internal/web/server.go` (route), `internal/web/admin_alerts.go` (limite 20000, `cleanMedia`), `internal/notify/push.go`, `web/templates/partials_dashboard.html`, `web/templates/avvisi.html`, `web/static/js/notifiche.js`
- Create: `internal/web/avviso.go`, `web/templates/avviso.html`
- Test: `internal/web/dashboard_test.go`, `internal/notify/push_test.go`, nuovo `internal/web/avviso_test.go`

**Interfaces:**
- Consumes: `markdown.Render`, `markdown.Plain`, `markdown.HasRich` (Task 2); `s.cleanMedia()` (Task 4)
- Produces: funzioni template `md` (`func(string) template.HTML` = `markdown.Render(s, markdown.Options{})`), `excerpt` (`func(string) string` = `markdown.Plain(s, 200)`), `needsMore` (`func(string) bool`: `Plain(s,0)` più lungo di 200 rune o `HasRich`); route `GET /avvisi/{id}`; `notify.Payload` con `body` = `markdown.Plain(body, 120)` e `url` = `/avvisi/{id}`.

- [ ] **Step 1: Test che falliscono**

```go
// internal/web/avviso_test.go
package web

import (
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestAlertPage(t *testing.T) {
	s, db := newTestServer(t, nil)
	id, _ := db.CreateAlert(database.Alert{Title: "Sciopero", Body: "## Dettagli\n\nUffici **chiusi**.", Level: "info", StartsAt: fixedNow.Add(-time.Hour)})
	rec := do(t, s, "GET", "/avvisi/"+itoa(id), nil, nil, nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "<h3") || !strings.Contains(body, "<strong>chiusi</strong>") {
		t.Fatalf("pagina avviso: %d\n%s", rec.Code, body)
	}
	future, _ := db.CreateAlert(database.Alert{Title: "Futuro", Level: "info", StartsAt: fixedNow.Add(time.Hour)})
	for _, target := range []string{"/avvisi/" + itoa(future), "/avvisi/9999", "/avvisi/abc"} {
		rec := do(t, s, "GET", target, nil, nil, nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Avviso non disponibile") {
			t.Errorf("%s: %d", target, rec.Code)
		}
	}
}

func TestCarouselExcerpt(t *testing.T) {
	s, db := newTestServer(t, nil)
	long := strings.Repeat("parola ", 60) // 420 caratteri
	id, _ := db.CreateAlert(database.Alert{Title: "Lungo", Body: "**Inizio** " + long, Level: "info", StartsAt: fixedNow.Add(-time.Hour)})
	db.CreateAlert(database.Alert{Title: "Breve", Body: "Solo *questo*.", Level: "info", StartsAt: fixedNow.Add(-time.Hour)})
	body := do(t, s, "GET", "/partials/alerts", nil, nil, nil).Body.String()
	if strings.Contains(body, "**Inizio**") || strings.Contains(body, strings.Repeat("parola ", 40)) {
		t.Fatalf("carosello non troncato:\n%s", body)
	}
	if !strings.Contains(body, `href="/avvisi/`+itoa(id)+`"`) || strings.Count(body, "Leggi tutto") != 1 {
		t.Fatalf("Leggi tutto solo per l'avviso lungo:\n%s", body)
	}
}
```

```go
// internal/notify/push_test.go (aggiungere)
func TestPayloadPlainAndURL(t *testing.T) {
	var p map[string]string
	json.Unmarshal(Payload(database.Alert{ID: 7, Title: "T", Body: "**Grassetto** e [link](https://x.org)"}), &p)
	if p["body"] != "Grassetto e link" || p["url"] != "/avvisi/7" {
		t.Fatalf("payload: %v", p)
	}
}
```

Nel test esistente del popup urgente in `dashboard_test.go`, aggiungere un caso: corpo `"**importante**"` → il dialog contiene `<strong>importante</strong>`.

- [ ] **Step 2: Verificare il fallimento**

Run: `go test ./internal/web/ -run 'AlertPage|CarouselExcerpt|Urgent' && go test ./internal/notify/ -run Payload`
Expected: FAIL.

- [ ] **Step 3: Funzioni template (`render.go`)**

```go
"md":        func(s string) template.HTML { return markdown.Render(s, markdown.Options{}) },
"excerpt":   func(s string) string { return markdown.Plain(s, excerptRunes) },
"needsMore": needsMore,
```

```go
const excerptRunes = 200

// needsMore: in carosello l'estratto non basta (testo troncato, immagini o tabelle).
func needsMore(body string) bool {
	return utf8.RuneCountInString(markdown.Plain(body, 0)) > excerptRunes || markdown.HasRich(body)
}
```

Rimuovere `paragraphs`/`linkify` e i loro test **solo se** non più usati altrove (cercare con `grep -rn "paragraphs\|linkify" internal web`); il calendario usa `Description` in testo semplice: se `linkify` è usato lì, lasciarlo.

- [ ] **Step 4: Template**

`partials_dashboard.html`, `alert_card` diventa parametrico: in carosello estratto, in `/avvisi` corpo intero. Usare due template:

```html
{{define "alert_head"}}
	<div class="news-head">
		<span class="news-level">{{levelLabel .Level}}</span>
		<span class="news-source">{{alertSource .Source}}</span>
		<span class="news-when">dal {{fmtDate .StartsAt}}{{if .EndsAt}} · fino al {{fmtDatePtr .EndsAt}}{{end}}</span>
	</div>
{{end}}

{{define "alert_card"}}
<article class="news news-{{.Level}}" data-alert="{{.ID}}">
	{{template "alert_head" .}}
	<h3 class="news-title">{{.Title}}</h3>
	{{if .Body}}<p class="news-body">{{excerpt .Body}}</p>{{end}}
	{{if needsMore .Body}}<a class="news-more" href="/avvisi/{{.ID}}">Leggi tutto →</a>{{end}}
</article>
{{end}}

{{define "alert_full"}}
<article class="news news-{{.Level}}" id="avviso-{{.ID}}">
	{{template "alert_head" .}}
	<h3 class="news-title">{{.Title}}</h3>
	{{if .Body}}<div class="news-body md">{{md .Body}}</div>{{end}}
</article>
{{end}}
```

Popup urgente: `{{if .Body}}<div class="urgent-body md">{{md .Body}}</div>{{end}}`.
`avvisi.html`: `{{range .Alerts}}{{template "alert_full" .}}…`.
Nuovo `avviso.html`: copia della struttura di `avvisi.html` (head, hero-compact con `← Tutti gli avvisi` verso `/avvisi`, footer, i due dialog delle notifiche) con `<main class="avvisi-list">{{with .Alert}}{{template "alert_full" .}}{{else}}<p class="empty">Avviso non disponibile: è scaduto o non è destinato a te.</p>{{end}}</main>`; `<title>{{with .Alert}}{{.Title}}{{else}}Avviso{{end}} · CruscottoPA{{template "title_ente"}}</title>`.

CSS in `plancia.css`: `.news-more` (link piccolo, colore `var(--p-blue-2)`), `.md` (immagini `max-width:100%; height:auto; border-radius:8px`, tabelle con bordi `var(--p-line)` e `overflow-x:auto` tramite `display:block`, `h2..h6` dimensioni ridotte, `code` come `.ff-help code`). Togliere `white-space: pre-line` da `.news-body` se c'è (ora gli a capo sono `<br>`).

- [ ] **Step 5: Handler `avviso.go`**

```go
package web

import (
	"net/http"
	"strconv"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type avvisoView struct {
	Alert   *database.Alert
	Version string
	Admin   bool
}

// handleAvviso: avviso attivo e visibile (stesse regole di /avvisi). Altrimenti
// "non disponibile" con 200: il proxy riscriverebbe un 404.
func (s *Server) handleAvviso(w http.ResponseWriter, r *http.Request) {
	view := avvisoView{Version: s.version, Admin: s.viewerIsAdmin(r)}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		alerts, err := s.db.ListActiveAlerts(s.now())
		if err != nil {
			s.serverError(w, err)
			return
		}
		f, err := s.contentFilterFor(r)
		if err != nil {
			s.serverError(w, err)
			return
		}
		for _, a := range f.alertList(alerts) {
			if a.ID == id {
				view.Alert = &a
				break
			}
		}
	}
	s.render(w, http.StatusOK, "avviso.html", view)
}
```

Route: `s.mux.HandleFunc("GET /avvisi/{id}", s.handleAvviso)`.

- [ ] **Step 6: Notifiche**

`notify/push.go`:

```go
func Payload(a database.Alert) []byte {
	b, _ := json.Marshal(map[string]string{
		"title": a.Title,
		"body":  markdown.Plain(a.Body, 120),
		"url":   fmt.Sprintf("/avvisi/%d", a.ID),
		"tag":   fmt.Sprintf("avviso-%d", a.ID),
	})
	return b
}
```

Aggiornare eventuali test esistenti su `Payload` che si aspettano 160 caratteri o `url: "/"`.
`notifiche.js` `onAvviso`: `data: { url: "/avvisi/" + a.id }`. `sw.js` `notificationclick`: se una plancia è aperta, `c.navigate(url)` poi `c.focus()` quando `url !== "/"`:

```js
for (const c of list) {
	if (new URL(c.url).origin === self.location.origin) {
		return (url === "/" ? Promise.resolve(c) : c.navigate(url)).then((w) => (w || c).focus());
	}
}
```

- [ ] **Step 7: Admin avvisi**

`admin_alerts.go`: `checkText(errs, "body", form.Body, 20000, false)`; dopo salvataggio ed eliminazione riusciti chiamare `s.cleanMedia()`. `admin_avvisi.html`: `<textarea name="body" maxlength="20000" rows="8" data-editor>`.

- [ ] **Step 8: Verificare**

Run: `go test ./...`
Expected: PASS (adeguare i test esistenti che cercavano `<p>` del vecchio `paragraphs` nei carosello/avvisi: ora il carosello ha l'estratto in `<p class="news-body">`).

- [ ] **Step 9: Commit**

```bash
git add internal web
git commit -m "Avvisi in Markdown: estratto in carosello, pagina dell'avviso, notifiche in testo semplice"
```

---

### Task 6: Pacchetto GitHub — `internal/guidesrc`

**Files:**
- Create: `internal/guidesrc/github.go`, `internal/guidesrc/fetch.go`, `internal/guidesrc/github_test.go`, `internal/guidesrc/fetch_test.go`

**Interfaces:**
- Produces:
  - `type Source struct { Raw, LinkBase, ImageBase string }`
  - `func ParseGitHubURL(s string) (Source, error)`; `var ErrBadURL`
  - `type Fetcher struct { Client *http.Client; MaxBytes int64 }`; `func NewFetcher() *Fetcher` (timeout 15 s, 1 MB, proxy da env, redirect limitati); `func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (string, error)`
  - errori utente in italiano: `ErrNotFound` ("File non trovato su GitHub (404)."), `ErrTooBig` ("File troppo grande (massimo 1 MB)."), `ErrNotText` ("Il file non è testo UTF-8."), redirect esterno → errore con "redirect non ammesso".

- [ ] **Step 1: Test che falliscono**

```go
// github_test.go
package guidesrc

import "testing"

func TestParseGitHubURL(t *testing.T) {
	src, err := ParseGitHubURL("https://github.com/Comune-x/Manuali/blob/main/docs/Posta Elettronica.md")
	if err == nil {
		t.Fatal("spazi non codificati: atteso errore")
	}
	src, err = ParseGitHubURL("https://github.com/org/repo/blob/main/docs/guida.md")
	if err != nil {
		t.Fatal(err)
	}
	want := Source{
		Raw:       "https://raw.githubusercontent.com/org/repo/main/docs/guida.md",
		LinkBase:  "https://github.com/org/repo/blob/main/docs/",
		ImageBase: "https://raw.githubusercontent.com/org/repo/main/docs/",
	}
	if src != want {
		t.Fatalf("%+v", src)
	}
	if src, _ := ParseGitHubURL("https://github.com/o/r/blob/v1.2/README.MD"); src.LinkBase != "https://github.com/o/r/blob/v1.2/" {
		t.Fatalf("file in radice: %+v", src)
	}
	for _, bad := range []string{
		"http://github.com/o/r/blob/main/a.md",
		"https://gitlab.com/o/r/blob/main/a.md",
		"https://user@github.com/o/r/blob/main/a.md",
		"https://github.com:8443/o/r/blob/main/a.md",
		"https://github.com/o/r/tree/main/docs",
		"https://github.com/o/r/blob/main/a.txt",
		"https://github.com/o/r/blob/main/../x/a.md",
		"https://github.com/o/r/blob/main/a.md?x=1",
		"https://raw.githubusercontent.com/o/r/main/a.md",
		"https://github.com/o/r",
		"",
	} {
		if _, err := ParseGitHubURL(bad); err == nil {
			t.Errorf("%q accettato", bad)
		}
	}
}
```

```go
// fetch_test.go
package guidesrc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testFetcher(srv *httptest.Server) *Fetcher {
	f := NewFetcher()
	f.Client.Transport = srv.Client().Transport
	f.allowHost = func(host string) bool { return host == strings.TrimPrefix(srv.URL, "https://") }
	return f
}

func TestFetch(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.md":
			w.Write([]byte("# Ciao"))
		case "/big.md":
			w.Write([]byte(strings.Repeat("a", 1<<20+1)))
		case "/bin.md":
			w.Write([]byte{0xff, 0xfe, 0x00})
		case "/redir.md":
			http.Redirect(w, r, "https://evil.example/x.md", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := testFetcher(srv)
	ctx := context.Background()
	if got, err := f.Fetch(ctx, srv.URL+"/ok.md"); err != nil || got != "# Ciao" {
		t.Fatalf("ok: %q %v", got, err)
	}
	if _, err := f.Fetch(ctx, srv.URL+"/no.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("404: %v", err)
	}
	if _, err := f.Fetch(ctx, srv.URL+"/big.md"); !errors.Is(err, ErrTooBig) {
		t.Errorf("grande: %v", err)
	}
	if _, err := f.Fetch(ctx, srv.URL+"/bin.md"); !errors.Is(err, ErrNotText) {
		t.Errorf("binario: %v", err)
	}
	if _, err := f.Fetch(ctx, srv.URL+"/redir.md"); err == nil || !strings.Contains(err.Error(), "redirect non ammesso") {
		t.Errorf("redirect esterno: %v", err)
	}
}
```

- [ ] **Step 2: Verificare il fallimento**

Run: `go test ./internal/guidesrc/`
Expected: FAIL (pacchetto vuoto).

- [ ] **Step 3: `github.go`**

```go
// Package guidesrc scarica le guide Markdown da GitHub (repository pubblici).
package guidesrc

import (
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var ErrBadURL = errors.New("Usa il link a un file .md su GitHub, es. https://github.com/org/repo/blob/main/docs/guida.md")

// Source: dove scaricare il file e come riscrivere link e immagini relativi.
type Source struct {
	Raw       string // https://raw.githubusercontent.com/{owner}/{repo}/{ref}/{path}
	LinkBase  string // https://github.com/{owner}/{repo}/blob/{ref}/{dir}/
	ImageBase string // https://raw.githubusercontent.com/{owner}/{repo}/{ref}/{dir}/
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func ParseGitHubURL(s string) (Source, error) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil ||
		u.RawQuery != "" || strings.ContainsAny(s, " \t\r\n") {
		return Source{}, ErrBadURL
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "blob" || !nameRe.MatchString(parts[0]) || !nameRe.MatchString(parts[1]) {
		return Source{}, ErrBadURL
	}
	for _, p := range parts[3:] {
		if p == "" || p == "." || p == ".." {
			return Source{}, ErrBadURL
		}
	}
	file := strings.Join(parts[4:], "/")
	if !strings.EqualFold(path.Ext(file), ".md") {
		return Source{}, ErrBadURL
	}
	owner, repo, ref := parts[0], parts[1], parts[3]
	dir := path.Dir(file)
	if dir == "." {
		dir = ""
	} else {
		dir += "/"
	}
	raw := "https://raw.githubusercontent.com/" + owner + "/" + repo + "/" + ref + "/"
	return Source{
		Raw:       raw + file,
		LinkBase:  "https://github.com/" + owner + "/" + repo + "/blob/" + ref + "/" + dir,
		ImageBase: raw + dir,
	}, nil
}
```

Nota: un ref con `/` (es. `feature/x`) non è distinguibile dal percorso: si assume il primo segmento come ref (limite documentato nell'aiuto del campo).

- [ ] **Step 4: `fetch.go`**

```go
package guidesrc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound = errors.New("File non trovato su GitHub (404).")
	ErrTooBig   = errors.New("File troppo grande (massimo 1 MB).")
	ErrNotText  = errors.New("Il file non è testo UTF-8.")
)

const rawHost = "raw.githubusercontent.com"

type Fetcher struct {
	Client    *http.Client
	MaxBytes  int64
	allowHost func(host string) bool // test: host del server finto
}

func NewFetcher() *Fetcher {
	f := &Fetcher{MaxBytes: 1 << 20, allowHost: func(h string) bool { return h == rawHost }}
	f.Client = &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 10 * time.Second},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || req.URL.Scheme != "https" || !f.allowHost(req.URL.Host) {
				return fmt.Errorf("redirect non ammesso verso %s", req.URL.Host)
			}
			return nil
		},
	}
	return f
}

func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "CruscottoPA")
	resp, err := f.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub non raggiungibile: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("GitHub ha risposto %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.MaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("download interrotto: %w", err)
	}
	if int64(len(data)) > f.MaxBytes {
		return "", ErrTooBig
	}
	if !utf8.Valid(data) {
		return "", ErrNotText
	}
	return string(data), nil
}
```

Nota: nel test `testFetcher` sostituisce `Transport` (che perde il proxy da env: ok nei test). `CheckRedirect` legge `f.allowHost` al momento della chiamata, quindi l'override nel test vale.

- [ ] **Step 5: Verificare**

Run: `go test ./internal/guidesrc/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/guidesrc
git commit -m "Download delle guide Markdown da GitHub"
```

---

### Task 7: Admin guide per tipo

**Files:**
- Modify: `internal/web/admin_guides.go`, `internal/web/server.go` (Options `GuideFetch`, route), `web/templates/admin_guide.html`, `web/static/js/admin.js`
- Test: `internal/web/admin_guides_test.go`

**Interfaces:**
- Consumes: `database.GuideKind*`, `ValidGuideKind`, `SetGuideFetched`, `SetGuideFetchError` (Task 3); `guidesrc.ParseGitHubURL`, `guidesrc.NewFetcher().Fetch` (Task 6); upload `/admin/media?tipo=pdf` (Task 4); `s.cleanMedia()`, `s.removeUpload(uploadGuide, name)` (estendere `removeUpload` per accettare `guideMediaRe` quando `kind == uploadGuide`).
- Produces:
  - `Options.GuideFetch func(ctx context.Context, rawURL string) (string, error)` — nil → `guidesrc.NewFetcher().Fetch`; campo `Server.fetchGuide`
  - `func (s *Server) refreshGuide(ctx context.Context, g database.Guide) error` — scarica e salva (`SetGuideFetched`) o registra l'errore (`SetGuideFetchError`), restituisce l'errore
  - route `POST /admin/guide/{id}/aggiorna`
  - campi form: `kind` (`link|markdown|pdf|github`), `url`, `body`, `file`, `source_url`

- [ ] **Step 1: Test che falliscono**

```go
// admin_guides_test.go (aggiungere)
func fakeFetch(body string, err error) func(*Options) {
	return func(o *Options) {
		o.GuideFetch = func(ctx context.Context, raw string) (string, error) { return body, err }
	}
}

func TestGuideKindsSave(t *testing.T) {
	s, db := newTestServerWith(t, nil, fakeFetch("# Da GitHub", nil))
	c := login(t, s)
	pdf := uploadMedia(t, s, c, "pdf", []byte("%PDF-1.7\n%%EOF"))["name"].(string)
	for _, form := range []url.Values{
		{"kind": {"markdown"}, "title": {"Interna"}, "body": {"**ciao**"}, "app_id": {"0"}, "enabled": {"1"}},
		{"kind": {"pdf"}, "title": {"Manuale"}, "file": {pdf}, "app_id": {"0"}, "enabled": {"1"}},
		{"kind": {"github"}, "title": {"Da repo"}, "source_url": {"https://github.com/o/r/blob/main/a.md"}, "app_id": {"0"}, "enabled": {"1"}},
	} {
		if rec := do(t, s, "POST", "/admin/guide", form, c, hx); rec.Code != 200 {
			t.Fatalf("%s: %d\n%s", form.Get("kind"), rec.Code, rec.Body)
		}
	}
	gs, _ := db.ListGuides()
	if len(gs) != 3 || gs[0].Body != "**ciao**" || gs[1].File != pdf || gs[2].Body != "# Da GitHub" || gs[2].FetchedAt == nil {
		t.Fatalf("DB: %+v", gs)
	}
}

func TestGuideKindValidation(t *testing.T) {
	s, _ := newTestServerWith(t, nil, fakeFetch("", guidesrc.ErrNotFound))
	c := login(t, s)
	for kind, form := range map[string]url.Values{
		"markdown vuota": {"kind": {"markdown"}, "title": {"X"}, "body": {" "}},
		"pdf assente":    {"kind": {"pdf"}, "title": {"X"}, "file": {"0123456789abcdef0123456789abcdef.pdf"}},
		"github url":     {"kind": {"github"}, "title": {"X"}, "source_url": {"https://gitlab.com/o/r/blob/main/a.md"}},
		"github 404":     {"kind": {"github"}, "title": {"X"}, "source_url": {"https://github.com/o/r/blob/main/a.md"}},
		"tipo":           {"kind": {"html"}, "title": {"X"}},
	} {
		if rec := do(t, s, "POST", "/admin/guide", form, c, hx); rec.Code != 422 {
			t.Errorf("%s: atteso 422, %d", kind, rec.Code)
		}
	}
}

func TestGuideRefreshNow(t *testing.T) {
	body, ferr := "# v1", error(nil)
	s, db := newTestServerWith(t, nil, func(o *Options) {
		o.GuideFetch = func(context.Context, string) (string, error) { return body, ferr }
	})
	c := login(t, s)
	do(t, s, "POST", "/admin/guide", url.Values{"kind": {"github"}, "title": {"G"}, "source_url": {"https://github.com/o/r/blob/main/a.md"}, "enabled": {"1"}}, c, hx)
	gs, _ := db.ListGuides()
	id := itoa(gs[0].ID)
	body = "# v2"
	do(t, s, "POST", "/admin/guide/"+id+"/aggiorna", nil, c, hx)
	g, _ := db.GetGuide(gs[0].ID)
	if g.Body != "# v2" {
		t.Fatalf("aggiorna: %q", g.Body)
	}
	ferr = errors.New("rete giù")
	rec := do(t, s, "POST", "/admin/guide/"+id+"/aggiorna", nil, c, hx)
	g, _ = db.GetGuide(gs[0].ID)
	if g.Body != "# v2" || g.FetchError == "" || !strings.Contains(rec.Body.String(), "Ultimo aggiornamento fallito") {
		t.Fatalf("errore: %+v\n%s", g, rec.Body)
	}
}

func TestGuidePDFReplacedRemovesOldFile(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	a := uploadMedia(t, s, c, "pdf", []byte("%PDF-1.7\n%%EOF"))["name"].(string)
	b := uploadMedia(t, s, c, "pdf", []byte("%PDF-1.7\n%%EOF"))["name"].(string)
	do(t, s, "POST", "/admin/guide", url.Values{"kind": {"pdf"}, "title": {"M"}, "file": {a}, "enabled": {"1"}}, c, hx)
	gs, _ := db.ListGuides()
	do(t, s, "POST", "/admin/guide/"+itoa(gs[0].ID), url.Values{"kind": {"pdf"}, "title": {"M"}, "file": {b}, "enabled": {"1"}}, c, hx)
	if _, err := os.Stat(filepath.Join(s.uploadDir(uploadGuide), a)); !os.IsNotExist(err) {
		t.Error("PDF vecchio non cancellato")
	}
	do(t, s, "POST", "/admin/guide/"+itoa(gs[0].ID)+"/elimina", nil, c, hx)
	if _, err := os.Stat(filepath.Join(s.uploadDir(uploadGuide), b)); !os.IsNotExist(err) {
		t.Error("PDF non cancellato con la guida")
	}
}
```

Aggiornare `TestGuidesCRUD`/`TestGuideValidation` esistenti: senza `kind` nel form il tipo predefinito è `link` (compatibilità), quindi restano validi.

- [ ] **Step 2: Verificare il fallimento**

Run: `go test ./internal/web/ -run Guide`
Expected: FAIL.

- [ ] **Step 3: Server — fetch iniettabile e refresh**

`server.go`: in `Options` aggiungere `GuideFetch func(ctx context.Context, rawURL string) (string, error)`; in `Server` `fetchGuide` con lo stesso tipo; in `New`: `if o.GuideFetch == nil { o.GuideFetch = guidesrc.NewFetcher().Fetch }`.

Nuovo file `internal/web/guide_refresh.go`:

```go
package web

import (
	"context"
	"log/slog"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/guidesrc"
)

// refreshGuide scarica la guida GitHub; se fallisce resta l'ultima copia buona
// e l'errore si vede nell'admin.
func (s *Server) refreshGuide(ctx context.Context, g database.Guide) error {
	src, err := guidesrc.ParseGitHubURL(g.SourceURL)
	if err == nil {
		var body string
		if body, err = s.fetchGuide(ctx, src.Raw); err == nil {
			return s.db.SetGuideFetched(g.ID, body, s.now())
		}
	}
	slog.Warn("guida GitHub non aggiornata", "guida", g.ID, "err", err)
	if serr := s.db.SetGuideFetchError(g.ID, err.Error()); serr != nil {
		return serr
	}
	return err
}
```

- [ ] **Step 4: Form e salvataggio**

`guideForm` aggiunge `Kind, Body, File, SourceURL string` e, per la visualizzazione, `FetchedAt *time.Time`, `FetchError string`. `formFromGuide` li copia. Nel salvataggio:

```go
form.Kind = r.FormValue("kind")
if form.Kind == "" {
	form.Kind = database.GuideKindLink
}
form.Body = strings.TrimSpace(strings.ReplaceAll(r.FormValue("body"), "\r\n", "\n"))
form.File = r.FormValue("file")
form.SourceURL = strings.TrimSpace(r.FormValue("source_url"))

var fetched string
switch form.Kind {
case database.GuideKindLink:
	checkURL(errs, "url", form.URL, true)
case database.GuideKindMarkdown:
	checkText(errs, "body", form.Body, 100000, true)
case database.GuideKindPDF:
	if !guideMediaRe.MatchString(form.File) || !strings.HasSuffix(form.File, ".pdf") || !s.mediaExists(form.File) {
		errs.add("file", "Carica il PDF.")
	}
case database.GuideKindGitHub:
	src, err := guidesrc.ParseGitHubURL(form.SourceURL)
	if err != nil {
		errs.add("source_url", err.Error())
		break
	}
	if len(errs) == 0 {
		if fetched, err = s.fetchGuide(r.Context(), src.Raw); err != nil {
			errs.add("source_url", "Download non riuscito: "+err.Error())
		}
	}
default:
	errs.add("kind", "Tipo di guida non valido.")
}
```

(il download GitHub va fatto **dopo** tutte le altre validazioni: spostare questo blocco in fondo, prima di `if len(errs) > 0`, e scaricare solo se `len(errs) == 0`.)

`mediaExists(name)`: `os.Stat(filepath.Join(s.uploadDir(uploadGuide), name))` senza errore.

Costruzione della guida: copiare solo i campi del tipo scelto (gli altri vuoti: `URL` solo per link, `Body` per markdown e github (= `fetched`), `File` per pdf, `SourceURL` per github). Prima dell'update leggere la guida corrente: se aveva un `File` diverso dal nuovo (o il tipo non è più pdf), dopo il salvataggio `s.removeUpload(uploadGuide, current.File)`. Dopo il salvataggio di una github: `s.db.SetGuideFetched(id, fetched, s.now())`. Poi `s.cleanMedia()`.

In eliminazione: leggere la guida, eliminare, `removeUpload` del PDF, `cleanMedia()`.

`removeUpload`: usare `guideMediaRe` quando `kind == uploadGuide`, `uploadFileRe` negli altri casi.

`handleGuideRefresh` (route `POST /admin/guide/{id}/aggiorna`): `GetGuide`; se non github → sezione con errore generale; altrimenti `s.refreshGuide(r.Context(), g)` (l'errore è già nel DB) e `renderGuides(w, 200, newGuideForm(), nil)`.

- [ ] **Step 5: Template `admin_guide.html`**

Nel form, prima del titolo:

```html
<fieldset class="kind-choice">
	<legend>Tipo</legend>
	<label><input type="radio" name="kind" value="link" data-guide-kind {{if eq .Form.Kind "link"}}checked{{end}}>Link</label>
	<label><input type="radio" name="kind" value="markdown" data-guide-kind {{if eq .Form.Kind "markdown"}}checked{{end}}>Testo</label>
	<label><input type="radio" name="kind" value="pdf" data-guide-kind {{if eq .Form.Kind "pdf"}}checked{{end}}>PDF</label>
	<label><input type="radio" name="kind" value="github" data-guide-kind {{if eq .Form.Kind "github"}}checked{{end}}>Da GitHub</label>
</fieldset>
<div data-kind-field="link"><label>Indirizzo (URL)<input name="url" value="{{.Form.URL}}" placeholder="https://…"></label>{{errorFor .Errors "url"}}</div>
<div data-kind-field="markdown"><label>Testo<textarea name="body" rows="14" data-editor>{{.Form.Body}}</textarea></label>{{errorFor .Errors "body"}}</div>
<div data-kind-field="pdf">
	<input type="hidden" name="file" value="{{.Form.File}}" data-pdf-name>
	<label>File PDF (max 20 MB)<input type="file" accept="application/pdf" data-pdf-upload></label>
	<p class="hint" data-pdf-status>{{if .Form.File}}PDF caricato.{{if .Form.ID}} <a href="/guide/{{.Form.ID}}/pdf" target="_blank" rel="noopener">Apri</a>{{end}}{{end}}</p>
	{{errorFor .Errors "file"}}
</div>
<div data-kind-field="github">
	<label>Link al file su GitHub<input name="source_url" value="{{.Form.SourceURL}}" placeholder="https://github.com/org/repo/blob/main/docs/guida.md"></label>
	<p class="hint">Solo repository pubblici. Il file viene scaricato ora e poi aggiornato periodicamente.</p>
	{{errorFor .Errors "source_url"}}
</div>
```

(usare il nome reale dell'helper d'errore già presente nei template admin: verificare con `grep -n "Errors" web/templates/admin_guide.html`; togliere `required` dall'input URL perché ora dipende dal tipo.) `newGuideForm()` imposta `Kind: database.GuideKindLink`.

In elenco, per ogni guida, accanto al titolo il tipo (`Link`/`Testo`/`PDF`/`GitHub`); per github: `aggiornata il {{fmtDatePtr .FetchedAt}}`, se `.FetchError` `<span class="error">Ultimo aggiornamento fallito: {{.FetchError}}</span>`, e un bottone `hx-post="/admin/guide/{{.ID}}/aggiorna" hx-target="#section" hx-swap="outerHTML"` "Aggiorna ora".

- [ ] **Step 6: `admin.js`**

```js
// Guide: mostra solo i campi del tipo scelto.
function syncGuideKind(root) {
	const checked = root.querySelector("[data-guide-kind]:checked");
	if (!checked) return;
	for (const el of root.querySelectorAll("[data-kind-field]")) el.hidden = el.dataset.kindField !== checked.value;
}
document.addEventListener("change", (e) => { if (e.target.matches("[data-guide-kind]")) syncGuideKind(e.target.form); });
document.addEventListener("htmx:afterSwap", (e) => syncGuideKind(e.target));
syncGuideKind(document);

// Upload a pezzi (immagini dell'editor e PDF delle guide). Risposte sempre 200+JSON.
async function uploadMedia(file, tipo) {
	const post = (url, body, type) => fetch(url, { method: "POST", credentials: "same-origin", headers: { "Content-Type": type }, body }).then((r) => r.json());
	const start = await post("/admin/media", "tipo=" + tipo, "application/x-www-form-urlencoded");
	if (!start.ok) throw new Error(start.error);
	for (let n = 0, off = 0; off < file.size; n++, off += start.chunk) {
		const r = await post(`/admin/media/${start.id}/pezzo?n=${n}`, file.slice(off, off + start.chunk), "application/octet-stream");
		if (!r.ok) throw new Error(r.error);
	}
	const end = await post(`/admin/media/${start.id}/fine`, "", "application/x-www-form-urlencoded");
	if (!end.ok) throw new Error(end.error);
	return end;
}
window.cruscottoUploadMedia = uploadMedia; // usato dall'editor (vendor/editor.js)

document.addEventListener("change", async (e) => {
	if (!e.target.matches("[data-pdf-upload]")) return;
	const form = e.target.form, status = form.querySelector("[data-pdf-status]");
	const file = e.target.files[0];
	if (!file) return;
	status.textContent = "Caricamento…";
	try {
		const r = await uploadMedia(file, "pdf");
		form.querySelector("[data-pdf-name]").value = r.name;
		status.textContent = "PDF caricato: " + file.name;
	} catch (err) {
		status.textContent = err.message && err.message.length < 120 ? err.message : "Caricamento non riuscito.";
	}
});
```

Nota: `err.message` arriva dal JSON del server (testo nostro); se la risposta è la pagina del proxy, `r.json()` fallisce → messaggio generico. `res.json()` di una pagina HTML lancia `SyntaxError` con messaggio lungo: per sicurezza mostrare il messaggio solo se è uno dei nostri (`err.name === "Error"`), altrimenti "Caricamento non riuscito.".

- [ ] **Step 7: Verificare**

Run: `go test ./internal/web/ && go vet ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/web web
git commit -m "Admin guide: testo, PDF e GitHub con aggiornamento manuale"
```

---

### Task 8: Guide in plancia — `/guide/{id}` e `/guide/{id}/pdf`

**Files:**
- Create: `internal/web/guides_public.go`, `internal/web/guides_public_test.go`, `web/templates/guida.html`
- Modify: `internal/web/server.go` (route), `internal/web/render.go` (`guideHref`, `guideTarget`), `internal/web/audience_filter.go` (`guideVisible`), `web/templates/partials_dashboard.html`

**Interfaces:**
- Consumes: `db.GetPlanciaGuide` (Task 3); `guidesrc.ParseGitHubURL` (Task 6); `markdown.Render` (Task 2); CSP del PDF decisa in Task 1.
- Produces: `func (f *contentFilter) guideVisible(g database.Guide) bool` (visibilità della guida e, se agganciata, della sua app; con `ShowAll` sempre true); funzioni template `guideHref` (`link` → `.URL`, `pdf` → `/guide/{id}/pdf`, altri → `/guide/{id}`) e `guideNewTab` (true per `link` e `pdf`).

- [ ] **Step 1: Test che falliscono**

```go
package web

import (
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestGuidePageMarkdownAndGitHub(t *testing.T) {
	s, db := newTestServer(t, nil)
	md, _ := db.CreateGuide(database.Guide{Title: "Interna", Kind: database.GuideKindMarkdown, Body: "# Passi\n\n1. uno", Enabled: true})
	gh, _ := db.CreateGuide(database.Guide{Title: "Repo", Kind: database.GuideKindGitHub, SourceURL: "https://github.com/o/r/blob/main/docs/a.md", Body: "![s](img/s.png)", Enabled: true})
	db.SetGuideFetched(gh, "![s](img/s.png)", fixedNow)
	body := do(t, s, "GET", "/guide/"+itoa(md), nil, nil, nil).Body.String()
	if !strings.Contains(body, "<h2") || !strings.Contains(body, "<ol>") || !strings.Contains(body, "Interna") {
		t.Fatalf("markdown:\n%s", body)
	}
	body = do(t, s, "GET", "/guide/"+itoa(gh), nil, nil, nil).Body.String()
	if !strings.Contains(body, `src="https://raw.githubusercontent.com/o/r/main/docs/img/s.png"`) || !strings.Contains(body, `href="https://github.com/o/r/blob/main/docs/a.md"`) {
		t.Fatalf("github:\n%s", body)
	}
}

func TestGuidePageVisibility(t *testing.T) {
	s, db := newTestServer(t, nil)
	apps, _ := db.ListApps() // seed: app senza URL → non visibili in plancia
	hidden := apps[0].ID
	off, _ := db.CreateGuide(database.Guide{Title: "Off", Kind: database.GuideKindMarkdown, Body: "x"})
	ofHidden, _ := db.CreateGuide(database.Guide{AppID: &hidden, Title: "H", Kind: database.GuideKindMarkdown, Body: "x", Enabled: true})
	restricted, _ := db.CreateGuide(database.Guide{Title: "Riservata", Kind: database.GuideKindMarkdown, Body: "segreto", Enabled: true})
	setOnlyGroup(t, db, database.ContentGuide, restricted) // helper esistente nei test di visibilità: guida solo per un gruppo
	for _, id := range []int64{off, ofHidden, restricted, 9999} {
		for _, suffix := range []string{"", "/pdf"} {
			rec := do(t, s, "GET", "/guide/"+itoa(id)+suffix, nil, nil, nil)
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Guida non disponibile") || strings.Contains(rec.Body.String(), "segreto") {
				t.Errorf("guida %d%s: %d", id, suffix, rec.Code)
			}
		}
	}
}

func TestGuidePDF(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	name := uploadMedia(t, s, c, "pdf", []byte("%PDF-1.7\n%%EOF"))["name"].(string)
	id, _ := db.CreateGuide(database.Guide{Title: "Manuale d'uso", Kind: database.GuideKindPDF, File: name, Enabled: true})
	rec := do(t, s, "GET", "/guide/"+itoa(id)+"/pdf", nil, nil, nil)
	h := rec.Header()
	if rec.Code != 200 || h.Get("Content-Type") != "application/pdf" || h.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.HasPrefix(h.Get("Content-Disposition"), "inline;") || h.Get("Content-Security-Policy") != pdfCSP {
		t.Fatalf("%d %v", rec.Code, h)
	}
}

func TestDashboardGuideLinks(t *testing.T) {
	s, db := newTestServer(t, nil)
	md, _ := db.CreateGuide(database.Guide{Title: "Interna", Kind: database.GuideKindMarkdown, Body: "x", Enabled: true})
	db.CreateGuide(database.Guide{Title: "Esterna", Kind: database.GuideKindLink, URL: "https://wiki.local/x", Enabled: true})
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	if !strings.Contains(body, `href="/guide/`+itoa(md)+`"`) || !strings.Contains(body, `href="https://wiki.local/x" target="_blank"`) {
		t.Fatalf("link guide:\n%s", body)
	}
}
```

(`setOnlyGroup`: cercare nei test di visibilità esistenti — `admin_visibility_test.go`/`audience_filter_test.go` — l'helper che crea un gruppo e assegna `only`; se non c'è, crearlo lì con `db.SetContentAudience`/le funzioni usate da quei test.)

- [ ] **Step 2: Verificare il fallimento**

Run: `go test ./internal/web/ -run 'GuidePage|GuidePDF|DashboardGuideLinks'`
Expected: FAIL.

- [ ] **Step 3: Implementazione**

```go
package web

import (
	"errors"
	"html/template"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/guidesrc"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/markdown"
)

// pdfCSP: scelta in base alla prova del viewer di Chrome/Edge (spec, sezione 6).
const pdfCSP = "sandbox; default-src 'none'; style-src 'unsafe-inline'" // sostituire con l'esito del Task 1

type guidaView struct {
	Guide   *database.Guide
	HTML    template.HTML
	Source  string // link al file su GitHub
	Version string
	Admin   bool
}

// planciaGuide: guida abilitata, di un'app visibile e destinata a chi guarda.
func (s *Server) planciaGuide(r *http.Request) (*database.Guide, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, nil
	}
	g, err := s.db.GetPlanciaGuide(id)
	if errors.Is(err, database.ErrNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	f, err := s.contentFilterFor(r)
	if err != nil {
		return nil, err
	}
	if !f.guideVisible(g) {
		return nil, nil
	}
	return &g, nil
}

func (s *Server) handleGuidePage(w http.ResponseWriter, r *http.Request) {
	g, err := s.planciaGuide(r)
	if err != nil {
		s.serverError(w, err)
		return
	}
	view := guidaView{Version: s.version, Admin: s.viewerIsAdmin(r)}
	switch {
	case g == nil:
	case g.Kind == database.GuideKindMarkdown:
		view.Guide, view.HTML = g, markdown.Render(g.Body, markdown.Options{})
	case g.Kind == database.GuideKindGitHub:
		src, _ := guidesrc.ParseGitHubURL(g.SourceURL)
		view.Guide, view.Source = g, g.SourceURL
		view.HTML = markdown.Render(g.Body, markdown.Options{LinkBase: src.LinkBase, ImageBase: src.ImageBase})
	case g.Kind == database.GuideKindLink:
		http.Redirect(w, r, g.URL, http.StatusSeeOther)
		return
	case g.Kind == database.GuideKindPDF:
		http.Redirect(w, r, "/guide/"+strconv.FormatInt(g.ID, 10)+"/pdf", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "guida.html", view)
}

func (s *Server) handleGuidePDF(w http.ResponseWriter, r *http.Request) {
	g, err := s.planciaGuide(r)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if g == nil || g.Kind != database.GuideKindPDF || !guideMediaRe.MatchString(g.File) {
		s.render(w, http.StatusOK, "guida.html", guidaView{Version: s.version, Admin: s.viewerIsAdmin(r)})
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", pdfCSP)
	h.Set("Content-Disposition", `inline; filename="`+pdfFilename(g.Title)+`"`)
	http.ServeFile(w, r, filepath.Join(s.uploadDir(uploadGuide), g.File))
}

// pdfFilename: titolo ridotto a caratteri sicuri per l'header.
func pdfFilename(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '\'':
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "guida.pdf"
	}
	return b.String() + ".pdf"
}
```

Nota: `securityHeaders` imposta già la CSP globale; qui la sovrascriviamo con `h.Set` (verificare che il middleware la scriva prima di chiamare l'handler, come per `/uploads/icons`).

`audience_filter.go`:

```go
// guideVisible: la guida e, se agganciata, la sua app sono destinate a chi guarda.
func (f *contentFilter) guideVisible(g database.Guide) bool {
	if f.ShowAll {
		return true
	}
	if g.AppID != nil && !f.visible(f.apps[*g.AppID]) {
		return false
	}
	return f.visible(f.guides[g.ID])
}
```

`render.go`:

```go
"guideHref": func(g database.Guide) string {
	switch g.Kind {
	case database.GuideKindLink:
		return g.URL
	case database.GuideKindPDF:
		return fmt.Sprintf("/guide/%d/pdf", g.ID)
	}
	return fmt.Sprintf("/guide/%d", g.ID)
},
"guideNewTab": func(g database.Guide) bool { return g.Kind == database.GuideKindLink || g.Kind == database.GuideKindPDF },
```

Template: in `partials_dashboard.html` sostituire i due link alle guide (flyout e `widget_guide`) con
`<a href="{{guideHref .}}"{{if guideNewTab .}} target="_blank" rel="noopener"{{end}}>{{.Title}}</a>`.

`guida.html`: struttura di `avviso.html` (Task 5) con hero-compact `← CruscottoPA`, `<h1 class="hello">{{with .Guide}}{{.Title}}{{else}}Guida{{end}}</h1>`, `<main class="guida md">{{if .Guide}}{{.HTML}}{{with .Source}}<p class="guida-fonte">Fonte: <a href="{{.}}" target="_blank" rel="noopener">GitHub</a>{{with $.Guide.FetchedAt}} · aggiornata il {{fmtDatePtr .}}{{end}}</p>{{end}}{{else}}<p class="empty">Guida non disponibile: non esiste più o non è destinata a te.</p>{{end}}</main>`, footer e dialog notifiche come gli altri. CSS `.guida` in `plancia.css`: larghezza di lettura (`max-width: 52rem; margin: 1.5rem auto; padding: 0 var(--p-gutter)`), sfondo bianco, `line-height: 1.6`.

Route:

```go
s.mux.HandleFunc("GET /guide/{id}", s.handleGuidePage)
s.mux.HandleFunc("GET /guide/{id}/pdf", s.handleGuidePDF)
```

Aggiornare l'elenco delle route pubbliche in `CLAUDE.md` (Task 11).

- [ ] **Step 4: Verificare**

Run: `go test ./internal/web/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web web/templates web/static/css
git commit -m "Pagine delle guide in plancia (Markdown, GitHub, PDF) con la visibilità dei gruppi"
```

---

### Task 9: Aggiornamento periodico delle guide GitHub

**Files:**
- Modify: `internal/config/config.go` (+test), `docker-compose.yml`, `.env.example`, `internal/web/guide_refresh.go`, `internal/web/server.go` (`StartNotifications` → avvio anche del refresh, oppure nuovo `StartGuideRefresh`), `cmd/server/main.go`
- Test: `internal/web/guide_refresh_test.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: `db.GuidesToRefresh`, `s.refreshGuide` (Task 3, 7)
- Produces: `Config.GuideRefreshHours int` (env `GUIDE_REFRESH_HOURS`, default 6, negativo = errore); `func (s *Server) RefreshGuides(ctx context.Context)` (un giro); `func (s *Server) StartGuideRefresh(ctx context.Context)` (goroutine con ticker ogni 10 min che chiama `RefreshGuides`; niente se `GuideRefreshHours == 0`).

- [ ] **Step 1: Test che falliscono**

```go
package web

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestRefreshGuides(t *testing.T) {
	calls := 0
	s, db := newTestServerWith(t, nil, func(o *Options) {
		o.Config.GuideRefreshHours = 6
		o.GuideFetch = func(context.Context, string) (string, error) { calls++; return "# nuovo", nil }
	})
	stale, _ := db.CreateGuide(database.Guide{Title: "Vecchia", Kind: database.GuideKindGitHub, SourceURL: "https://github.com/o/r/blob/main/a.md", Body: "# vecchio", Enabled: true})
	db.SetGuideFetched(stale, "# vecchio", fixedNow.Add(-7*time.Hour))
	fresh, _ := db.CreateGuide(database.Guide{Title: "Fresca", Kind: database.GuideKindGitHub, SourceURL: "https://github.com/o/r/blob/main/b.md", Body: "# fresco", Enabled: true})
	db.SetGuideFetched(fresh, "# fresco", fixedNow.Add(-time.Hour))
	s.RefreshGuides(context.Background())
	if g, _ := db.GetGuide(stale); g.Body != "# nuovo" {
		t.Errorf("vecchia non aggiornata: %q", g.Body)
	}
	if g, _ := db.GetGuide(fresh); g.Body != "# fresco" || calls != 1 {
		t.Errorf("fresca toccata: %q, chiamate %d", g.Body, calls)
	}
}

func TestRefreshKeepsBodyOnError(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) {
		o.Config.GuideRefreshHours = 6
		o.GuideFetch = func(context.Context, string) (string, error) { return "", errors.New("timeout") }
	})
	id, _ := db.CreateGuide(database.Guide{Title: "G", Kind: database.GuideKindGitHub, SourceURL: "https://github.com/o/r/blob/main/a.md", Body: "# buona", Enabled: true})
	s.RefreshGuides(context.Background())
	g, _ := db.GetGuide(id)
	if g.Body != "# buona" || g.FetchError == "" {
		t.Fatalf("%+v", g)
	}
}
```

Nota: una guida con `fetch_error` e `fetched_at` vecchio verrebbe ritentata a ogni giro (10 min). Accettato: è un GET piccolo; il log `Warn` ripetuto è il segnale per l'admin.

In `config_test.go`: `GUIDE_REFRESH_HOURS` assente → 6; `"0"` → 0; `"-1"` → errore.

- [ ] **Step 2: Verificare il fallimento**

Run: `go test ./internal/web/ -run Refresh && go test ./internal/config/`
Expected: FAIL.

- [ ] **Step 3: Implementazione**

`config.go` (accanto a `BackupIntervalHours`):

```go
if cfg.GuideRefreshHours, err = getEnvInt("GUIDE_REFRESH_HOURS", 6); err != nil {
	return cfg, err
}
if cfg.GuideRefreshHours < 0 {
	return cfg, fmt.Errorf("GUIDE_REFRESH_HOURS non può essere negativo")
}
```

(adeguare alla forma di ritorno errori già usata in `Load`.)

`docker-compose.yml`: `- GUIDE_REFRESH_HOURS=${GUIDE_REFRESH_HOURS:-6}`. `.env.example`: `# Ogni quante ore riscaricare le guide da GitHub (0 = solo "Aggiorna ora")` + `GUIDE_REFRESH_HOURS=6`. Aggiungere anche un commento per `HTTPS_PROXY` (opzionale, se il server esce tramite proxy) e la variabile `- HTTPS_PROXY=${HTTPS_PROXY:-}` in compose.

`guide_refresh.go`:

```go
// RefreshGuides aggiorna le guide GitHub più vecchie dell'intervallo, una alla volta.
func (s *Server) RefreshGuides(ctx context.Context) {
	if s.cfg.GuideRefreshHours <= 0 {
		return
	}
	todo, err := s.db.GuidesToRefresh(s.now().Add(-time.Duration(s.cfg.GuideRefreshHours) * time.Hour))
	if err != nil {
		slog.Error("guide GitHub: elenco", "err", err)
		return
	}
	for _, g := range todo {
		if ctx.Err() != nil {
			return
		}
		s.refreshGuide(ctx, g) // errore già registrato sulla guida
	}
}

func (s *Server) StartGuideRefresh(ctx context.Context) {
	if s.cfg.GuideRefreshHours <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			s.RefreshGuides(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
```

`cmd/server/main.go`: `srv.StartGuideRefresh(ctx)` accanto a `srv.StartNotifications(ctx)`.

- [ ] **Step 4: Verificare**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal cmd docker-compose.yml .env.example
git commit -m "Aggiornamento periodico delle guide GitHub (GUIDE_REFRESH_HOURS)"
```

---

### Task 10: Editor visuale TipTap, build in container, anteprima

**Files:**
- Create: `web/editor/package.json`, `web/editor/pnpm-lock.yaml` (generato), `web/editor/build.mjs`, `web/editor/src/editor.js`, `web/editor/src/editor.css`, `scripts/editor.sh`, `internal/web/admin_preview.go`, `internal/web/admin_preview_test.go`
- Modify: `Dockerfile`, `.gitignore`, `.dockerignore` (se presente), `.github/dependabot.yml`, `.github/workflows/test.yml`, `web/templates/admin_base.html`, `internal/web/server.go` (route)

**Interfaces:**
- Consumes: `window.cruscottoUploadMedia(file, "immagine")` da `admin.js` (Task 7) → `{ok, url}`; `markdown.Render` (Task 2).
- Produces: `web/static/vendor/editor.js` + `editor.css` (generati, non versionati); `POST /admin/anteprima` (form `body`, opzionale) → sempre 200, HTML di `markdown.Render` dentro `<div class="md preview">…</div>`.

- [ ] **Step 1: Test anteprima (fallisce)**

```go
package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestAdminPreview(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	rec := do(t, s, "POST", "/admin/anteprima", url.Values{"body": {"**ciao** <script>x</script>"}}, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<strong>ciao</strong>") || strings.Contains(rec.Body.String(), "<script") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, "POST", "/admin/anteprima", url.Values{"body": {"x"}}, nil, hx); strings.Contains(rec.Body.String(), "<p>x</p>") {
		t.Fatal("anteprima senza login")
	}
}
```

Run: `go test ./internal/web/ -run AdminPreview` → FAIL.

- [ ] **Step 2: Handler**

```go
package web

import (
	"fmt"
	"net/http"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/markdown"
)

// handlePreview: lo stesso rendering della plancia, per l'editor.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<div class="md preview">%s</div>`, markdown.Render(r.FormValue("body"), markdown.Options{}))
}
```

Route `s.mux.HandleFunc("POST /admin/anteprima", s.requireAdmin(s.handlePreview))`. Run test → PASS.

- [ ] **Step 3: Pacchetto editor**

`web/editor/package.json`:

```json
{
  "name": "cruscottopa-editor",
  "private": true,
  "type": "module",
  "packageManager": "pnpm@12.10.1",
  "engines": { "node": ">=24" },
  "scripts": { "build": "node build.mjs" },
  "dependencies": {
    "@tiptap/core": "3.31.4",
    "@tiptap/pm": "3.31.4",
    "@tiptap/starter-kit": "3.31.4",
    "@tiptap/markdown": "3.31.4",
    "@tiptap/extension-image": "3.31.4",
    "@tiptap/extension-table": "3.31.4",
    "@tiptap/extensions": "3.31.4"
  },
  "devDependencies": { "esbuild": "0.28.2" }
}
```

(verificare con Context7/`npm view` le API di `@tiptap/markdown` 3.31: opzione `contentType: "markdown"` del costruttore e `editor.getMarkdown()`; adattare `editor.js` se i nomi differiscono.)

`web/editor/build.mjs`:

```js
import { build } from "esbuild";

await build({
	entryPoints: ["src/editor.js"],
	bundle: true,
	minify: true,
	format: "iife",
	target: "es2022",
	outfile: process.env.OUT_DIR ? `${process.env.OUT_DIR}/editor.js` : "dist/editor.js",
	legalComments: "linked",
});
await build({
	entryPoints: ["src/editor.css"],
	bundle: true,
	minify: true,
	outfile: process.env.OUT_DIR ? `${process.env.OUT_DIR}/editor.css` : "dist/editor.css",
});
```

`web/editor/src/editor.js`:

```js
// Editor visuale per avvisi e guide: sostituisce le <textarea data-editor>.
// Salva Markdown nella textarea a ogni modifica (i form HTMX restano uguali).
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import Image from "@tiptap/extension-image";
import { TableKit } from "@tiptap/extension-table";
import { Placeholder } from "@tiptap/extensions";
import { Markdown } from "@tiptap/markdown";

const BUTTONS = [
	["Grassetto", "format_bold", (e) => e.chain().focus().toggleBold().run(), (e) => e.isActive("bold")],
	["Corsivo", "format_italic", (e) => e.chain().focus().toggleItalic().run(), (e) => e.isActive("italic")],
	["Titolo", "title", (e) => e.chain().focus().toggleHeading({ level: 2 }).run(), (e) => e.isActive("heading", { level: 2 })],
	["Sottotitolo", "text_fields", (e) => e.chain().focus().toggleHeading({ level: 3 }).run(), (e) => e.isActive("heading", { level: 3 })],
	["Elenco puntato", "format_list_bulleted", (e) => e.chain().focus().toggleBulletList().run(), (e) => e.isActive("bulletList")],
	["Elenco numerato", "format_list_numbered", (e) => e.chain().focus().toggleOrderedList().run(), (e) => e.isActive("orderedList")],
	["Link", "link", linkPrompt, (e) => e.isActive("link")],
	["Immagine", "image", pickImage, null],
	["Tabella", "table_chart", (e) => e.chain().focus().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run(), null],
	["Aggiungi riga", "playlist_add", (e) => e.chain().focus().addRowAfter().run(), null, (e) => e.isActive("table")],
	["Aggiungi colonna", "view_column", (e) => e.chain().focus().addColumnAfter().run(), null, (e) => e.isActive("table")],
	["Elimina tabella", "delete", (e) => e.chain().focus().deleteTable().run(), null, (e) => e.isActive("table")],
	["Annulla", "undo", (e) => e.chain().focus().undo().run(), null],
	["Ripeti", "redo", (e) => e.chain().focus().redo().run(), null],
];

function linkPrompt(e) {
	const prev = e.getAttributes("link").href || "https://";
	const href = window.prompt("Indirizzo del link (vuoto per toglierlo):", prev);
	if (href === null) return;
	if (href.trim() === "") e.chain().focus().unsetLink().run();
	else e.chain().focus().extendMarkRange("link").setLink({ href: href.trim() }).run();
}

async function insertImages(e, files, status) {
	for (const f of files) {
		if (!f.type.startsWith("image/")) continue;
		status.textContent = "Caricamento immagine…";
		try {
			const r = await window.cruscottoUploadMedia(f, "immagine");
			e.chain().focus().setImage({ src: r.url, alt: f.name.replace(/\.[^.]+$/, "") }).run();
			status.textContent = "";
		} catch (err) {
			status.textContent = err && err.name === "Error" && err.message.length < 120 ? err.message : "Caricamento non riuscito.";
		}
	}
}

function pickImage(e, status) {
	const input = document.createElement("input");
	input.type = "file";
	input.accept = "image/png,image/jpeg,image/webp";
	input.addEventListener("change", () => insertImages(e, input.files, status));
	input.click();
}

function mount(ta) {
	if (ta.dataset.editorReady) return;
	ta.dataset.editorReady = "1";
	const wrap = document.createElement("div");
	wrap.className = "md-editor";
	const bar = document.createElement("div");
	bar.className = "md-toolbar";
	bar.setAttribute("role", "toolbar");
	const area = document.createElement("div");
	const status = document.createElement("p");
	status.className = "md-status hint";
	status.setAttribute("aria-live", "polite");
	ta.after(wrap);
	wrap.append(bar, area, status);
	ta.hidden = true;

	const editor = new Editor({
		element: area,
		extensions: [
			StarterKit.configure({ heading: { levels: [2, 3] }, link: { openOnClick: false, autolink: true } }),
			Image,
			TableKit,
			Placeholder.configure({ placeholder: "Scrivi qui…" }),
			Markdown,
		],
		content: ta.value,
		contentType: "markdown",
		editorProps: {
			attributes: { class: "md md-editor-area", "aria-label": ta.closest("label")?.textContent.trim() || "Testo" },
			handlePaste: (view, ev) => {
				const files = [...(ev.clipboardData?.files || [])];
				if (!files.length) return false;
				insertImages(editor, files, status);
				return true;
			},
			handleDrop: (view, ev) => {
				const files = [...(ev.dataTransfer?.files || [])];
				if (!files.length) return false;
				ev.preventDefault();
				insertImages(editor, files, status);
				return true;
			},
		},
		onUpdate: ({ editor }) => { ta.value = editor.getMarkdown(); },
	});

	const buttons = BUTTONS.map(([label, icon, run, active, shown]) => {
		const b = document.createElement("button");
		b.type = "button";
		b.title = label;
		b.setAttribute("aria-label", label);
		b.innerHTML = `<span class="material-icons" aria-hidden="true">${icon}</span>`;
		b.addEventListener("click", () => run(editor, status));
		bar.append(b);
		return { b, active, shown };
	});
	const sync = () => {
		for (const { b, active, shown } of buttons) {
			if (active) b.setAttribute("aria-pressed", active(editor) ? "true" : "false");
			if (shown) b.hidden = !shown(editor);
		}
	};
	editor.on("selectionUpdate", sync);
	editor.on("transaction", sync);
	sync();

	// Il textarea nascosto è "required"? Il browser non può segnalarlo: il server valida comunque.
	ta.removeAttribute("required");
}

function mountAll(root) {
	for (const ta of root.querySelectorAll("textarea[data-editor]")) mount(ta);
}
document.addEventListener("htmx:afterSwap", (e) => mountAll(e.target));
mountAll(document);
```

Nota: `innerHTML` con `icon` costante del modulo (nessun input utente). Il testo d'errore mostrato è solo il messaggio JSON nostro (`err.name === "Error"`).

`web/editor/src/editor.css`: stili di `.md-editor` (bordo `1px solid #cbd5e1`, radius 8px), `.md-toolbar` (flex, wrap, bottoni 32×32, `aria-pressed="true"` con sfondo azzurro chiaro), `.md-editor-area` (`min-height: 12rem; padding: .75rem; outline: none`), `.ProseMirror p.is-editor-empty:first-child::before { content: attr(data-placeholder); color: #94a3b8; float: left; height: 0; pointer-events: none; }`, tabelle con bordi, `img { max-width: 100% }`, `.ProseMirror-selectednode { outline: 2px solid #3b82f6 }`, `.tableWrapper { overflow-x: auto }`, `.column-resize-handle` nascosto.

- [ ] **Step 4: Script locale e Dockerfile**

`scripts/editor.sh`:

```bash
#!/bin/sh
# Costruisce l'editor (web/static/vendor/editor.{js,css}) con Node in container:
# Node non serve sul PC. Uso: scripts/editor.sh   (da Git Bash: MSYS_NO_PATHCONV=1)
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$root/web/static/vendor"
docker run --rm \
	-v "$root/web/editor":/src:ro \
	-v "$root/web/static/vendor":/out \
	node:24-alpine sh -ec '
		cp -r /src /w && cd /w
		corepack enable
		pnpm install --frozen-lockfile
		OUT_DIR=/out pnpm build
	'
```

Prima volta (lockfile assente): generare `pnpm-lock.yaml` con lo stesso container montando `web/editor` in scrittura: `MSYS_NO_PATHCONV=1 docker run --rm -v "$PWD/web/editor":/w -w /w node:24-alpine sh -ec 'corepack enable && pnpm install'` e committare il lockfile. Togliere `node_modules` creato (`rm -rf web/editor/node_modules`) e aggiungerlo a `.gitignore`.

`.gitignore`: `web/static/vendor/`, `web/editor/node_modules/`, `web/editor/dist/`.

`Dockerfile`, stage nuovo in testa:

```dockerfile
# ── Stage 0: Editor (Node solo qui) ──────────────────────────────────────────
FROM node:24-alpine AS editor
WORKDIR /w
COPY web/editor/package.json web/editor/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/editor/ ./
RUN OUT_DIR=/out pnpm build
```

e nello stage finale, dopo `COPY web/ ./web/`:

```dockerfile
COPY --from=editor /out/ ./web/static/vendor/
```

Se esiste `.dockerignore`, verificare che `web/editor/node_modules` sia escluso.

- [ ] **Step 5: Caricamento nell'admin**

`admin_base.html`, dopo `admin.css` e `admin.js`:

```html
<link rel="stylesheet" href="/static/vendor/editor.css">
<script src="/static/vendor/editor.js" defer></script>
```

Senza bundle (sviluppo con solo `go run`) i due 404 lasciano la textarea: comportamento voluto. `admin.js` deve essere caricato **prima** di `editor.js` (entrambi `defer`: l'ordine nel documento è rispettato) perché l'editor usa `window.cruscottoUploadMedia`.

Bottone anteprima, sotto ogni textarea con editor nei form di avvisi e guide:

```html
<button type="button" class="secondary" hx-post="/admin/anteprima" hx-include="closest form" hx-target="next .preview-slot" hx-swap="innerHTML">Anteprima</button>
<div class="preview-slot"></div>
```

(`hx-include="closest form"` invia anche `body`; l'editor scrive nella textarea a ogni modifica.) CSS `.preview` in `admin.css`: bordo tratteggiato, padding, stessi stili `.md` della plancia (copiare le regole `.md` in `app.css`, condiviso, invece che in `plancia.css`).

- [ ] **Step 6: CI e Dependabot**

`test.yml`, nuovo step dopo `Test` (stesso job `test`):

```yaml
      # L'editor si costruisce solo in container (stage "editor" del Dockerfile):
      # una PR di Dependabot npm che rompe la build diventa rossa qui.
      - name: Build editor
        run: docker build --target editor .
```

`dependabot.yml`:

```yaml
  - package-ecosystem: npm
    directory: "/web/editor"
    schedule:
      interval: weekly
    cooldown:
      default-days: 7
      semver-major-days: 14
    groups:
      tiptap:
        patterns: ["@tiptap/*"]
```

(il gruppo evita PR separate per pacchetti TipTap che vanno tenuti alla stessa versione.)

- [ ] **Step 7: Build e verifica nel browser**

Run (Git Bash): `MSYS_NO_PATHCONV=1 sh scripts/editor.sh && ls -la web/static/vendor`
Expected: `editor.js` (qualche centinaio di KB) e `editor.css`.

Poi `LDAP_HOST=mock go run ./cmd/server`, con Playwright:
1. `/admin/avvisi`: l'editor sostituisce la textarea; grassetto, elenco, link, tabella funzionano; incollare un'immagine (`browser_run_code_unsafe` con `DataTransfer` o upload via bottone) → compare con URL `/uploads/guide/…`.
2. Salvare → in plancia carosello con estratto e "Leggi tutto"; `/avvisi/{id}` con immagine e tabella.
3. `browser_console_messages`: **nessun** errore "Content Security Policy".
4. Rinominare temporaneamente `web/static/vendor` → la textarea normale funziona e il salvataggio va.
5. `/admin/guide`: tipo Testo con editor; tipo PDF con upload (file di 1–2 MB) → in plancia la guida apre il PDF in Chrome (e Edge se disponibile).

Annotare nel commit gli esiti.

- [ ] **Step 8: Verificare build immagine**

Run: `docker build --build-arg VERSION=x -t cp:check . && docker run -d --name cpcheck -p 18090:8080 -e LDAP_HOST=mock cp:check && curl -s localhost:18090/static/vendor/editor.js | head -c 100; docker rm -f cpcheck`
Expected: il bundle è servito dall'immagine.

- [ ] **Step 9: Commit**

```bash
git add web/editor scripts/editor.sh Dockerfile .gitignore .github internal/web web/templates web/static
git commit -m "Editor visuale TipTap per avvisi e guide, costruito in container"
```

---

### Task 11: Documentazione, revisione finale, rilascio v0.7.0

**Files:**
- Modify: `CLAUDE.md`, `README.md`, `publiccode.yml`, `docs/superpowers/specs/2026-10-07-guide-markdown-design.md` (Stato: implementata)

- [ ] **Step 1: `CLAUDE.md`**

- Progetto: sotto-progetto 2 fatto.
- Comandi: `MSYS_NO_PATHCONV=1 sh scripts/editor.sh` (editor in container; senza, l'admin usa la textarea).
- Architettura: paragrafi `internal/markdown` (goldmark, HTML grezzo off, URL ammessi, `#`→`h2`, hard wraps, `Plain`/`HasRich`), `internal/guidesrc` (URL ammessi, 1 MB, 15 s, redirect solo raw, `HTTPS_PROXY`), guide per tipo + migrazione v7, media a pezzi (`/admin/media`, 2 MB immagini, 20 MB PDF, pulizia orfani > 24 h, `.tmp` escluso dal backup), editor TipTap (stage `editor`, `web/static/vendor` non versionato, Dependabot npm gruppo tiptap), `GUIDE_REFRESH_HOURS`.
- Route pubbliche: aggiungere `/avvisi/{id}`, `/guide/{id}`, `/guide/{id}/pdf`, `/uploads/guide/{file}`.
- Vincoli di deploy: niente da aggiungere se non `HTTPS_PROXY` opzionale.

- [ ] **Step 2: README**: sezione sviluppo con `scripts/editor.sh`; tipi di guida.

- [ ] **Step 3: Verifica completa**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS (il test flaky `TestCleanupCompletesInterruptedSwap` di `internal/backup` può fallire su Windows: rilanciare; in CI è verde).

- [ ] **Step 4: Revisione dell'intero branch** (subagent reviewer sul modello più capace), correggere i finding importanti con test RED→GREEN.

- [ ] **Step 5: Commit, PR, merge, tag**

```bash
git add -A CLAUDE.md README.md publiccode.yml docs
git commit -m "Documentazione: guide ricche e avvisi in Markdown"
git push -u origin feat/guide-markdown
gh pr create --title "Guide ricche e avvisi in Markdown con editor visuale (0.7.0)" --body "…"
gh pr checks --watch
```

Al via dell'utente: `publiccode.yml` `softwareVersion: 0.7.0` e `releaseDate`, squash merge `--delete-branch`, tag annotato `v0.7.0` su `origin/main`, verifica `release.yml`.
