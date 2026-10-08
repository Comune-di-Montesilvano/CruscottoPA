package web

import (
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// searchItem: una voce della ricerca a tendina della plancia. L'indice è
// scritto nella pagina (JSON, non eseguito) dopo il filtro per gruppi.
type searchItem struct {
	Kind   string `json:"k"` // app | guide | support
	Title  string `json:"t"`
	Sub    string `json:"s,omitempty"` // categoria, app della guida, app coperte
	Text   string `json:"x,omitempty"` // altro testo cercabile (descrizione, nota)
	URL    string `json:"u"`
	NewTab bool   `json:"n,omitempty"`
	Icon   string `json:"i,omitempty"` // Material Icon (app con icona del catalogo)
	Color  string `json:"c,omitempty"`
}

func buildSearchIndex(d database.Dashboard) []searchItem {
	out := []searchItem{}
	type chanApps struct {
		c    database.SupportChannel
		apps []string
	}
	var chans []*chanApps
	seen := map[int64]*chanApps{}
	for _, c := range d.Categories {
		for _, a := range c.Apps {
			it := searchItem{Kind: "app", Title: a.Title, Sub: c.Name, Text: a.Description, URL: a.URL, NewTab: true, Color: a.IconColor}
			if a.IconKind == database.IconPack {
				it.Icon = a.IconValue
			}
			out = append(out, it)
			for _, g := range a.Guides {
				out = append(out, searchItem{Kind: "guide", Title: g.Title, Sub: a.Title, URL: guideHref(g), NewTab: guideNewTab(g)})
			}
			for _, sc := range a.Support {
				ca, ok := seen[sc.ID]
				if !ok {
					ca = &chanApps{c: sc}
					seen[sc.ID] = ca
					chans = append(chans, ca)
				}
				ca.apps = append(ca.apps, a.Title)
			}
		}
	}
	for _, g := range d.GeneralGuides {
		out = append(out, searchItem{Kind: "guide", Title: g.Title, Sub: "Guida generale", URL: guideHref(g), NewTab: guideNewTab(g)})
	}
	for _, ca := range chans {
		out = append(out, searchItem{Kind: "support", Title: ca.c.Title, Sub: strings.Join(ca.apps, ", "), Text: ca.c.Note, URL: ca.c.URL, NewTab: true})
	}
	// Il JS usa l'URL come href: solo http/https o percorsi interni.
	safe := out[:0]
	for _, it := range out {
		if indexURLSafe(it.URL) {
			safe = append(safe, it)
		}
	}
	return safe
}

func indexURLSafe(u string) bool {
	return validURL(u) || (strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//") && !strings.HasPrefix(u, "/\\"))
}
