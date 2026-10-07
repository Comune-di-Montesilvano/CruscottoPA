package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type visibilityField struct {
	Mode     string
	Selected map[int64]bool
	Groups   []database.AudienceGroup
}

func (s *Server) visibilityField(ca database.ContentAudience) (visibilityField, error) {
	groups, err := s.db.ListAudienceGroups()
	f := visibilityField{Mode: string(ca.Mode), Selected: map[int64]bool{}, Groups: groups}
	for _, g := range ca.Groups {
		f.Selected[g] = true
	}
	return f, err
}

// parseVisibility legge "visibilita" e "gruppi"; con Pubblico i gruppi sono ignorati.
func parseVisibility(r *http.Request, errs formErrors) database.ContentAudience {
	mode := r.FormValue("visibilita")
	if !audience.ValidMode(mode) {
		errs.add("visibilita", "Visibilità non valida.")
		return database.ContentAudience{Groups: []int64{}}
	}
	ca := database.ContentAudience{Mode: audience.Mode(mode), Groups: []int64{}}
	if ca.Mode == audience.ModePublic {
		return ca
	}
	for _, v := range r.Form["gruppi"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			ca.Groups = append(ca.Groups, id)
		}
	}
	if len(ca.Groups) == 0 {
		errs.add("visibilita", "Scegli almeno un gruppo.")
	}
	return ca
}

// visibilityLabels: testo per gli elenchi admin ("" = pubblico).
func (s *Server) visibilityLabels(k database.ContentKind) (map[int64]string, error) {
	all, err := s.db.AllContentAudience(k)
	if err != nil {
		return nil, err
	}
	groups, err := s.db.ListAudienceGroups()
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, g := range groups {
		names[g.ID] = g.Name
	}
	out := map[int64]string{}
	for id, ca := range all {
		var ns []string
		for _, g := range ca.Groups {
			ns = append(ns, names[g])
		}
		prefix := "Riservato: "
		if ca.Mode == audience.ModeHide {
			prefix = "Nascosto a: "
		}
		out[id] = prefix + strings.Join(ns, ", ")
	}
	return out, nil
}

// checkGroups: ogni gruppo scelto deve esistere.
func (s *Server) checkGroups(ca database.ContentAudience, errs formErrors) error {
	groups, err := s.db.ListAudienceGroups()
	if err != nil {
		return err
	}
	exists := map[int64]bool{}
	for _, g := range groups {
		exists[g.ID] = true
	}
	for _, id := range ca.Groups {
		if !exists[id] {
			errs.add("visibilita", "Gruppo non valido.")
			break
		}
	}
	return nil
}
