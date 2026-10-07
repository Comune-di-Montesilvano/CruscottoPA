package web

import (
	"net/http"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// showAllCookie: preferenza "Mostra tutto", impostata da dashboard.js.
const showAllCookie = "cruscotto_tutto"

// contentFilter applica la visibilità dei contenuti a chi guarda. È
// presentazione, non sicurezza: con "Mostra tutto" si vede ogni contenuto.
type contentFilter struct {
	memberOf             map[int64]bool
	known, ShowAll       bool
	Hidden               int
	apps, guides, alerts map[int64]database.ContentAudience
}

func (s *Server) contentFilterFor(r *http.Request) (*contentFilter, error) {
	f := &contentFilter{}
	f.memberOf, f.known = s.viewerGroups(r)
	if c, err := r.Cookie(showAllCookie); err == nil && c.Value == "1" {
		f.ShowAll = true
	}
	var err error
	if f.apps, err = s.db.AllContentAudience(database.ContentApp); err != nil {
		return nil, err
	}
	if f.guides, err = s.db.AllContentAudience(database.ContentGuide); err != nil {
		return nil, err
	}
	if f.alerts, err = s.db.AllContentAudience(database.ContentAlert); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *contentFilter) visible(ca database.ContentAudience) bool {
	return audience.Visible(ca.Mode, ca.Groups, f.memberOf, f.known)
}

// keep conta ciò che esclude e dice se l'elemento resta in pagina.
func (f *contentFilter) keep(visible bool) bool {
	if visible || f.ShowAll {
		return true
	}
	f.Hidden++
	return false
}

func (f *contentFilter) dashboard(d *database.Dashboard) {
	d.Alerts = f.alertList(d.Alerts)
	cats := d.Categories[:0]
	for _, c := range d.Categories {
		kept := c.Apps[:0]
		for _, a := range c.Apps {
			if !f.keep(f.visible(f.apps[a.ID])) {
				continue
			}
			gs := a.Guides[:0]
			for _, g := range a.Guides {
				if f.keep(f.visible(f.guides[g.ID])) {
					gs = append(gs, g)
				}
			}
			a.Guides = gs
			kept = append(kept, a)
		}
		if len(kept) > 0 {
			c.Apps = kept
			cats = append(cats, c)
		}
	}
	d.Categories = cats
	gen := d.GeneralGuides[:0]
	for _, g := range d.GeneralGuides {
		if f.keep(f.visible(f.guides[g.ID])) {
			gen = append(gen, g)
		}
	}
	d.GeneralGuides = gen
}

// alertList: con "Mostra tutto" gli avvisi non destinati restano, marcati,
// così non aprono il popup degli urgenti.
func (f *contentFilter) alertList(list []database.Alert) []database.Alert {
	out := list[:0]
	for _, a := range list {
		v := f.visible(f.alerts[a.ID])
		if !f.keep(v) {
			continue
		}
		a.NotForViewer = !v
		out = append(out, a)
	}
	return out
}
