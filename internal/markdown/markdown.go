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

// Render: a capo singolo = <br> (gli avvisi scritti in testo semplice restano
// come prima), "#" diventa <h2> (l'<h1> è il titolo della pagina).
func Render(src string, opt Options) template.HTML {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(sanitizer{opt}, 100))),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)
	var b bytes.Buffer
	if err := md.Convert([]byte(src), &b); err != nil {
		return template.HTML("<p>" + template.HTMLEscapeString(src) + "</p>") //nolint:gosec // escapato
	}
	return template.HTML(b.String()) //nolint:gosec // HTML grezzo disattivato, URL filtrati da sanitizer
}

type sanitizer struct{ opt Options }

func (s sanitizer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	var drop, unwrap []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			n.Level = min(n.Level+1, 6)
		case *ast.Link:
			dest, ok := fixURL(string(n.Destination), s.opt.LinkBase, true)
			if !ok {
				unwrap = append(unwrap, n)
				return ast.WalkContinue, nil
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
			if _, ok := fixURL(u, "", true); !ok {
				drop = append(drop, n)
				return ast.WalkSkipChildren, nil
			}
			external(n, u)
		case *ast.Image:
			dest, ok := fixURL(string(n.Destination), s.opt.ImageBase, false)
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

// fixURL valida un URL e riscrive i relativi con base. link=false (immagini):
// niente mailto.
func fixURL(raw, base string, link bool) (string, bool) {
	raw = strings.TrimSpace(raw)
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
		if base == "" || raw == "" || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "#") {
			return raw, true
		}
		return base + strings.TrimPrefix(raw, "./"), true
	}
	return "", false
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
