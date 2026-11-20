// Package markdown rende il Markdown di avvisi e guide in HTML sicuro.
// HTML grezzo disattivato; URL ammessi: http, https, mailto (solo link), relativi.
package markdown

import (
	"bytes"
	stdhtml "html"
	"html/template"
	"net/url"
	"strconv"
	"strings"
	"unicode"
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
// Vuote = relativi lasciati come sono. Root: radice del repository, per i
// percorsi che iniziano con "/" (su GitHub sono relativi al repository).
type Options struct {
	LinkBase, ImageBase string
	LinkRoot, ImageRoot string
}

// Render: a capo singolo = <br> (gli avvisi scritti in testo semplice restano
// come prima), "#" diventa <h2> (l'<h1> è il titolo della pagina).
func Render(src string, opt Options) template.HTML {
	return render(src, sanitizer{opt: opt})
}

// Heading: voce dell'indice di una guida. Level è quello scritto nel
// Markdown (2 = "##", 3 = "###"), prima dello spostamento di un livello.
type Heading struct {
	Level int
	ID    string
	Text  string
}

// AnchorPrefix: anteposto agli id dei titoli, perché non si scontrino con gli
// id della pagina usati dagli script.
const AnchorPrefix = "md-"

// RenderDoc: come Render, per una pagina con un solo documento (guide). I
// titoli prendono un id come su GitHub (AnchorPrefix + slug, doppioni -1, -2)
// e i link "#slug" puntano lì; restituisce i titoli "##" e "###" per l'indice.
func RenderDoc(src string, opt Options) (template.HTML, []Heading) {
	d := &docState{used: map[string]int{}}
	return render(src, sanitizer{opt: opt, doc: d}), d.heads
}

type docState struct {
	used  map[string]int
	heads []Heading
}

func (d *docState) id(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			b.WriteRune(r)
		}
	}
	slug := b.String()
	n := d.used[slug]
	d.used[slug] = n + 1
	if n > 0 {
		slug += "-" + strconv.Itoa(n)
	}
	return AnchorPrefix + slug
}

func render(src string, s sanitizer) template.HTML {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(s, 100))),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)
	var b bytes.Buffer
	if err := md.Convert([]byte(src), &b); err != nil {
		return template.HTML("<p>" + template.HTMLEscapeString(src) + "</p>") //nolint:gosec // escapato
	}
	return template.HTML(b.String()) //nolint:gosec // HTML grezzo disattivato, URL filtrati da sanitizer
}

type sanitizer struct {
	opt Options
	doc *docState // nil = niente id sui titoli
}

func (s sanitizer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	var drop, unwrap []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			if s.doc != nil {
				text := strings.Join(strings.Fields(nodeText(n, src)), " ")
				id := s.doc.id(text)
				n.SetAttributeString("id", []byte(id))
				if n.Level == 2 || n.Level == 3 {
					s.doc.heads = append(s.doc.heads, Heading{Level: n.Level, ID: id, Text: text})
				}
			}
			n.Level = min(n.Level+1, 6)
		case *ast.Link:
			dest, ok := fixURL(string(n.Destination), s.opt.LinkBase, s.opt.LinkRoot, true)
			if !ok {
				unwrap = append(unwrap, n)
				return ast.WalkContinue, nil
			}
			if s.doc != nil && strings.HasPrefix(dest, "#") && len(dest) > 1 {
				dest = "#" + AnchorPrefix + strings.TrimPrefix(dest, "#")
			}
			n.Destination = []byte(dest)
			external(n, dest)
		case *ast.AutoLink:
			if n.AutoLinkType == ast.AutoLinkEmail {
				return ast.WalkContinue, nil
			}
			u := string(n.URL(src))
			if !strings.Contains(u, "://") { // linkify di "www.esempio.it": goldmark antepone http://
				u = "http://" + u
			}
			if _, ok := fixURL(u, "", "", true); !ok {
				drop = append(drop, n)
				return ast.WalkSkipChildren, nil
			}
			external(n, u)
		case *ast.Image:
			dest, ok := fixURL(string(n.Destination), s.opt.ImageBase, s.opt.ImageRoot, false)
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
			n.RemoveChild(n, c)
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

// fixURL valida un URL e riscrive i relativi con base (o root se iniziano con
// "/"). link=false (immagini): niente mailto. Le entità HTML si decodificano
// prima del controllo e fino in fondo (&amp;#x2F; → &#x2F; → /): il renderer
// le decodificherebbe dopo, trasformando un relativo innocuo in //host.
func fixURL(raw, base, root string, link bool) (string, bool) {
	raw, ok := unescapeAll(strings.TrimSpace(raw))
	if !ok {
		return "", false
	}
	if strings.ContainsAny(raw, "\x00\t\r\n\\") {
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
		if strings.HasPrefix(raw, "/") && root != "" {
			return root + strings.TrimPrefix(raw, "/"), true
		}
		if base == "" || raw == "" || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "#") {
			return raw, true
		}
		return base + strings.TrimPrefix(raw, "./"), true
	}
	return "", false
}

// unescapeAll decodifica le entità finché il testo non cambia più; troppi
// livelli di codifica = URL scartato.
func unescapeAll(s string) (string, bool) {
	for range 4 {
		u := stdhtml.UnescapeString(s)
		if u == s {
			return s, true
		}
		s = u
	}
	return "", false
}

// nodeText: testo di un nodo senza sintassi (titoli con enfasi, codice, link).
func nodeText(n ast.Node, src []byte) string {
	var out strings.Builder
	_ = ast.Walk(n, func(nd ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch nd := nd.(type) {
		case *ast.Text:
			out.Write(nd.Segment.Value(src))
		case *ast.String:
			out.Write(nd.Value)
		case *ast.AutoLink:
			out.Write(nd.Label(src))
		}
		return ast.WalkContinue, nil
	})
	return out.String()
}

func parse(src []byte) ast.Node {
	return goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(src))
}

// Plain: testo senza sintassi Markdown, spazi compattati, troncato a n rune
// (n ≤ 0 = intero) sull'ultimo spazio, con "…".
func Plain(src string, n int) string {
	b := []byte(src)
	var out strings.Builder
	_ = ast.Walk(parse(b), func(nd ast.Node, entering bool) (ast.WalkStatus, error) {
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

// HasRich: il contenuto ha immagini, tabelle o link, che l'estratto in testo
// semplice perde (in carosello serve "Leggi tutto").
func HasRich(src string) bool {
	rich := false
	_ = ast.Walk(parse([]byte(src)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch k := n.Kind(); {
		case !entering:
		case k == ast.KindImage, k == extast.KindTable, k == ast.KindLink, k == ast.KindAutoLink:
			rich = true
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return rich
}
