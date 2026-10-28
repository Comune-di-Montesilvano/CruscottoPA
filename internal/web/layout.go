package web

import (
	"log/slog"
	"net/http"
	"sort"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// Preferiti e ordine personale della plancia: logica pura, applicata dopo il
// filtro per gruppi. Solo le app destinate all'utente contano.

const (
	favMax      = 6  // posti nei preferiti
	favMinOpens = 10 // aperture per entrare tra gli automatici
	// Giorni di aperture che contano (oggi compreso): chi cambia lavoro non si
	// porta dietro le app di prima. Oltre, la pulizia giornaliera le cancella.
	favOpensDays = 90
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
// le app più aperte con almeno favMinOpens aperture, a parità nell'ordine di
// pagina. Contano anche le app non destinate (in d con «Mostra tutto», altrimenti
// in others): chi le apre spesso le vuole, e nei preferiti si vedono come le altre.
func favorites(d database.Dashboard, others []database.AppWithGuides, pinned []int64, opens map[int64]int) []tileView {
	var cands []database.AppWithGuides
	byID := map[int64]database.AppWithGuides{}
	add := func(a database.AppWithGuides) {
		a.NotForViewer = false
		cands = append(cands, a)
		byID[a.ID] = a
	}
	for _, c := range d.Categories {
		for _, a := range c.Apps {
			add(a)
		}
	}
	for _, a := range others {
		add(a)
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

// layoutView: preferiti in cima alla plancia e stato per i riquadri.
type layoutView struct {
	Favorites []tileView
	Full      bool           // favMax fissati visibili: «Fissa» disattivato
	Max       int            // favMax, per personalizza.js
	pinned    map[int64]bool // fissati visibili
}

// Tile: riquadro di una categoria. Su nil (non riconosciuto) niente puntina.
func (l *layoutView) Tile(a database.AppWithGuides) tileView {
	t := tileView{AppWithGuides: a}
	if l != nil && !a.NotForViewer {
		t.Pinnable, t.Pinned = true, l.pinned[a.ID]
	}
	return t
}

// layoutFor applica l'ordine personale a d (già filtrata da f) e calcola i
// preferiti; nil per chi non è riconosciuto. Un errore del DB lascia la plancia com'è.
func (s *Server) layoutFor(user string, d *database.Dashboard, f *contentFilter) *layoutView {
	if user == "" {
		return nil
	}
	l, err := s.db.UserLayout(user)
	if err != nil {
		slog.Warn("preferiti", "err", err)
		return nil
	}
	opens, err := s.db.AppOpens(user, s.dayOf(s.now().AddDate(0, 0, -(favOpensDays-1))))
	if err != nil {
		slog.Warn("preferiti", "err", err)
		return nil
	}
	raw, err := s.db.GetDashboard(s.now())
	if err != nil {
		slog.Warn("preferiti", "err", err)
		return nil
	}
	applyLayout(d, l)
	v := &layoutView{Favorites: favorites(*d, f.appsOutside(raw, *d), l.Favorites, opens), pinned: map[int64]bool{}}
	for _, f := range v.Favorites {
		if f.Pinned {
			v.pinned[f.ID] = true
		}
	}
	v.Full, v.Max = len(v.pinned) >= favMax, favMax
	return v
}

// handleFavoritesPartial: la sola sezione dei preferiti (dopo un salvataggio
// in «Personalizza»). Vuota per chi non è riconosciuto, sempre 200.
func (s *Server) handleFavoritesPartial(w http.ResponseWriter, r *http.Request) {
	d, err := s.db.GetDashboard(s.now())
	if err != nil {
		s.serverError(w, err)
		return
	}
	f, err := s.contentFilterFor(r)
	if err != nil {
		s.serverError(w, err)
		return
	}
	f.dashboard(&d)
	s.markHot(&d)
	user, _ := s.viewerName(r)
	s.render(w, http.StatusOK, "favorites", s.layoutFor(user, &d, f))
}
