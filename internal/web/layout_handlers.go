package web

import (
	"net/http"
	"sort"
	"strconv"
)

// layoutMaxIDs: id accettati in un salvataggio di «Personalizza».
const layoutMaxIDs = 200

// targetedApps: app in plancia destinate a chi guarda (senza «Mostra tutto»),
// con la loro categoria.
func (s *Server) targetedApps(r *http.Request) (map[int64]int64, error) {
	d, err := s.db.GetDashboard(s.now())
	if err != nil {
		return nil, err
	}
	f, err := s.contentFilterFor(r)
	if err != nil {
		return nil, err
	}
	out := map[int64]int64{}
	for _, c := range d.Categories {
		for _, a := range c.Apps {
			if f.visible(f.apps[a.ID]) {
				out[a.ID] = c.ID
			}
		}
	}
	return out, nil
}

// layoutRequest legge utente e id inviati; false (e risposta già scritta) se
// chi guarda non è riconosciuto o il modulo non si legge.
func (s *Server) layoutRequest(w http.ResponseWriter, r *http.Request) (string, []int64, bool) {
	user, _ := s.viewerName(r)
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if user == "" || r.ParseForm() != nil || len(r.PostForm["id"]) > layoutMaxIDs {
		pushReply(w, false)
		return "", nil, false
	}
	var ids []int64
	for _, v := range r.PostForm["id"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return user, ids, true
}

// sortedByPosition: le chiavi di m in ordine di posizione.
func sortedByPosition(m map[int64]int, keep func(int64) bool) []int64 {
	var out []int64
	for id := range m {
		if keep(id) {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return m[out[i]] < m[out[j]] })
	return out
}

// handleSaveFavorites: i fissati nell'ordine inviato (al massimo favMax tra
// quelli destinati), poi i fissati salvati che la pagina non mostrava (nascosti
// o oltre i favMax visibili), nel loro ordine.
func (s *Server) handleSaveFavorites(w http.ResponseWriter, r *http.Request) {
	user, sent, ok := s.layoutRequest(w, r)
	if !ok {
		return
	}
	apps, err := s.targetedApps(r)
	l, err2 := s.db.UserLayout(user)
	if err != nil || err2 != nil {
		pushReply(w, false)
		return
	}
	visible := map[int64]bool{}
	for id := range apps {
		visible[id] = true
	}
	sent = mergeOrder(nil, sent, visible)
	if len(sent) > favMax {
		sent = sent[:favMax]
	}
	// Solo i fissati in pagina (i primi favMax visibili) dipendono da sent.
	shown := map[int64]bool{}
	for _, id := range l.Favorites {
		if visible[id] && len(shown) < favMax {
			shown[id] = true
		}
	}
	out := append([]int64{}, sent...)
	inSent := map[int64]bool{}
	for _, id := range sent {
		inSent[id] = true
	}
	for _, id := range l.Favorites {
		if !inSent[id] && !shown[id] {
			out = append(out, id)
		}
	}
	pushReply(w, s.db.SaveFavorites(user, out) == nil)
}

// handleSaveCategoryOrder: ordine delle categorie con almeno un'app destinata.
func (s *Server) handleSaveCategoryOrder(w http.ResponseWriter, r *http.Request) {
	user, sent, ok := s.layoutRequest(w, r)
	if !ok {
		return
	}
	apps, err := s.targetedApps(r)
	l, err2 := s.db.UserLayout(user)
	if err != nil || err2 != nil {
		pushReply(w, false)
		return
	}
	visible := map[int64]bool{}
	for _, c := range apps {
		visible[c] = true
	}
	saved := sortedByPosition(l.Categories, func(int64) bool { return true })
	pushReply(w, s.db.SaveCategoryOrder(user, mergeOrder(saved, sent, visible)) == nil)
}

// handleSaveAppOrder: ordine delle app destinate di una categoria; le altre
// app della categoria con una posizione salvata vanno in fondo.
func (s *Server) handleSaveAppOrder(w http.ResponseWriter, r *http.Request) {
	user, sent, ok := s.layoutRequest(w, r)
	if !ok {
		return
	}
	cat, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		pushReply(w, false)
		return
	}
	apps, err := s.targetedApps(r)
	l, err2 := s.db.UserLayout(user)
	all, err3 := s.db.ListApps()
	if err != nil || err2 != nil || err3 != nil {
		pushReply(w, false)
		return
	}
	inCat := map[int64]bool{}
	for _, a := range all {
		if a.CategoryID == cat {
			inCat[a.ID] = true
		}
	}
	visible := map[int64]bool{}
	for id, c := range apps {
		if c == cat {
			visible[id] = true
		}
	}
	saved := sortedByPosition(l.Apps, func(id int64) bool { return inCat[id] })
	pushReply(w, s.db.SaveAppOrder(user, cat, mergeOrder(saved, sent, visible)) == nil)
}

// handleResetLayout: «Ripristina» toglie fissati e ordine personale.
func (s *Server) handleResetLayout(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.layoutRequest(w, r)
	if !ok {
		return
	}
	pushReply(w, s.db.ResetLayout(user) == nil)
}
