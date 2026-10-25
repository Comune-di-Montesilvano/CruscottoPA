package web

import (
	"sort"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// Preferiti e ordine personale della plancia: logica pura, applicata dopo il
// filtro per gruppi. Solo le app destinate all'utente contano.

const (
	favMax      = 6  // posti nei preferiti
	favMinOpens = 10 // aperture per entrare tra gli automatici
)

// mergeOrder: l'ordine inviato (solo elementi visibili, senza ripetizioni),
// poi quelli salvati che ora non si vedono, nel loro ordine: chi sparisce per
// un po' non perde il posto e ricompare in fondo.
func mergeOrder(saved, sent []int64, visible map[int64]bool) []int64 {
	out := []int64{}
	seen := map[int64]bool{}
	for _, id := range sent {
		if visible[id] && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range saved {
		if !visible[id] && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// byUser: prima gli elementi con una posizione personale, poi gli altri
// nell'ordine attuale (quello dell'admin).
func byUser(pos map[int64]int, a, b int64) bool {
	pa, oka := pos[a]
	pb, okb := pos[b]
	switch {
	case oka && okb:
		return pa < pb
	default:
		return oka && !okb
	}
}

// applyLayout riordina categorie e app secondo l'utente.
func applyLayout(d *database.Dashboard, l database.Layout) {
	sort.SliceStable(d.Categories, func(i, j int) bool {
		return byUser(l.Categories, d.Categories[i].ID, d.Categories[j].ID)
	})
	for ci := range d.Categories {
		apps := d.Categories[ci].Apps
		sort.SliceStable(apps, func(i, j int) bool { return byUser(l.Apps, apps[i].ID, apps[j].ID) })
	}
}

// tileView: un riquadro della plancia. Prefix rende unici gli id HTML della
// copia nei preferiti; Pinnable = si può fissare (utente riconosciuto, app destinata).
type tileView struct {
	database.AppWithGuides
	Prefix                string
	Fav, Pinned, Pinnable bool
}

// favorites: fissati visibili nell'ordine dell'utente (al massimo favMax), poi
// le app più aperte con almeno favMinOpens aperture, a parità nell'ordine di pagina.
func favorites(d database.Dashboard, pinned []int64, opens map[int64]int) []tileView {
	var cands []database.AppWithGuides
	byID := map[int64]database.AppWithGuides{}
	for _, c := range d.Categories {
		for _, a := range c.Apps {
			if !a.NotForViewer {
				cands = append(cands, a)
				byID[a.ID] = a
			}
		}
	}
	out := []tileView{}
	taken := map[int64]bool{}
	for _, id := range pinned {
		if a, ok := byID[id]; ok && !taken[id] && len(out) < favMax {
			taken[id] = true
			out = append(out, tileView{AppWithGuides: a, Prefix: "fav-", Fav: true, Pinned: true, Pinnable: true})
		}
	}
	var auto []database.AppWithGuides
	for _, a := range cands {
		if !taken[a.ID] && opens[a.ID] >= favMinOpens {
			auto = append(auto, a)
		}
	}
	sort.SliceStable(auto, func(i, j int) bool { return opens[auto[i].ID] > opens[auto[j].ID] })
	for _, a := range auto {
		if len(out) == favMax {
			break
		}
		out = append(out, tileView{AppWithGuides: a, Prefix: "fav-", Fav: true, Pinnable: true})
	}
	return out
}
