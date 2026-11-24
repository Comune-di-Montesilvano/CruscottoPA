package web

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

const (
	adminLogPage      = 100 // righe per pagina
	adminLogRetention = 12  // mesi
)

type adminLogView struct {
	Rows             []database.AdminLogEntry
	Users, Sections  []string
	Chi, Sezione     string
	Page             int
	PrevURL, NextURL string
}

func (s *Server) handleAdminRegistro(w http.ResponseWriter, r *http.Request) {
	v := adminLogView{Chi: r.URL.Query().Get("chi"), Sezione: r.URL.Query().Get("sezione"), Page: 1}
	if n, err := strconv.Atoi(r.URL.Query().Get("pagina")); err == nil && n > 0 && n < 100000 {
		v.Page = n
	}
	rows, more, err := s.db.ListAdminLog(database.AdminLogFilter{
		Username: v.Chi, Section: v.Sezione, Limit: adminLogPage, Offset: (v.Page - 1) * adminLogPage,
	})
	if err != nil {
		s.serverError(w, err)
		return
	}
	if v.Users, v.Sections, err = s.db.AdminLogFacets(); err != nil {
		s.serverError(w, err)
		return
	}
	v.Rows = rows
	if v.Page > 1 {
		v.PrevURL = adminLogURL(v.Chi, v.Sezione, v.Page-1)
	}
	if more {
		v.NextURL = adminLogURL(v.Chi, v.Sezione, v.Page+1)
	}
	s.renderPage(w, r, "admin_registro.html", "registro", v)
}

// adminLogURL: pagina del registro mantenendo i filtri.
func adminLogURL(chi, sezione string, page int) string {
	q := url.Values{}
	if chi != "" {
		q.Set("chi", chi)
	}
	if sezione != "" {
		q.Set("sezione", sezione)
	}
	if page > 1 {
		q.Set("pagina", strconv.Itoa(page))
	}
	if len(q) == 0 {
		return "/admin/registro"
	}
	return "/admin/registro?" + q.Encode()
}

// cleanupAdminLog: giro giornaliero, righe più vecchie di 12 mesi.
func (s *Server) cleanupAdminLog() {
	if err := s.db.CleanupAdminLog(s.now().AddDate(0, -adminLogRetention, 0)); err != nil {
		slog.Warn("pulizia registro admin", "err", err)
	}
}
