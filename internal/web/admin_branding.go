package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type brandingSection struct {
	Branding database.Branding // valori salvati (anteprima del logo attuale)
	EnteName string            // valore del campo (in caso di errore, quello digitato)
	// Ricerca dalla plancia (in caso di errore, quanto digitato).
	WebSearch, SiteSearch string
	Errors                formErrors
	Saved                 bool
}

func (s *Server) handleBrandingPage(w http.ResponseWriter, r *http.Request) {
	b := s.ente()
	s.renderPage(w, r, "admin_ente.html", "ente", brandingSection{Branding: b, EnteName: b.EnteName, WebSearch: b.WebSearch, SiteSearch: b.SiteSearch})
}

func (s *Server) renderBranding(w http.ResponseWriter, status int, sec brandingSection) {
	sec.Branding = s.ente()
	s.render(w, status, "branding_section", sec)
}

func (s *Server) handleBrandingSave(w http.ResponseWriter, r *http.Request) {
	// Limite sul corpo intero: logo (512 KB) + margine per gli altri campi.
	r.Body = http.MaxBytesReader(w, r.Body, maxIconBytes+64<<10)
	if err := r.ParseMultipartForm(maxIconBytes + 64<<10); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			s.renderBranding(w, http.StatusUnprocessableEntity, brandingSection{EnteName: s.ente().EnteName, WebSearch: s.ente().WebSearch, SiteSearch: s.ente().SiteSearch, Errors: formErrors{"logo": errIconSize.Error()}})
			return
		}
		http.Error(w, "Richiesta non valida", http.StatusBadRequest)
		return
	}

	// Dalla lettura del valore attuale alla pulizia del vecchio logo: due
	// salvataggi insieme disallineerebbero cache, database e file.
	s.brandingMu.Lock()
	defer s.brandingMu.Unlock()
	current := s.ente()
	next := current
	next.EnteName = strings.TrimSpace(r.FormValue("ente_name"))
	errs := formErrors{}
	checkText(errs, "ente_name", next.EnteName, 120, false)
	next.WebSearch = strings.TrimSpace(r.FormValue("web_search"))
	if next.WebSearch == "" {
		next.WebSearch = database.DefaultWebSearch
	}
	next.SiteSearch = strings.TrimSpace(r.FormValue("site_search"))
	checkSearchURL(errs, "web_search", next.WebSearch)
	if next.SiteSearch != "" {
		checkSearchURL(errs, "site_search", next.SiteSearch)
	}

	// Il file si salva solo se il resto del form è valido: niente file orfani.
	var uploaded string
	if len(errs) == 0 {
		file, _, err := r.FormFile("logo_file")
		switch {
		case err == nil:
			name, saveErr := s.saveUpload(uploadBranding, file)
			file.Close()
			switch {
			case errors.Is(saveErr, errIconType), errors.Is(saveErr, errIconSize):
				errs.add("logo", saveErr.Error())
			case saveErr != nil:
				s.serverError(w, saveErr)
				return
			default:
				uploaded = name
			}
		case errors.Is(err, http.ErrMissingFile), errors.Is(err, http.ErrNotMultipart):
		default:
			s.serverError(w, err)
			return
		}
	}
	if len(errs) > 0 {
		s.renderBranding(w, http.StatusUnprocessableEntity, brandingSection{EnteName: next.EnteName, WebSearch: next.WebSearch, SiteSearch: next.SiteSearch, Errors: errs})
		return
	}

	switch {
	case uploaded != "": // un file nuovo vince su "Rimuovi"
		next.LogoFile = uploaded
	case r.FormValue("remove_logo") == "1":
		next.LogoFile = ""
	}
	next.UpdatedAt = s.now()
	next.UpdatedBy = s.currentAdmin(r)
	if err := s.db.UpdateBranding(next); err != nil {
		s.removeUpload(uploadBranding, uploaded)
		s.serverError(w, err)
		return
	}
	s.branding.Store(&next)
	if current.LogoFile != "" && current.LogoFile != next.LogoFile {
		s.removeUpload(uploadBranding, current.LogoFile)
	}
	s.renderBranding(w, http.StatusOK, brandingSection{EnteName: next.EnteName, WebSearch: next.WebSearch, SiteSearch: next.SiteSearch, Saved: true})
}

// checkSearchURL: indirizzo https con %s (una volta sola) al posto del testo.
func checkSearchURL(e formErrors, field, value string) {
	if len(value) > 500 || strings.Count(value, "%s") != 1 || !strings.HasPrefix(value, "https://") ||
		!validURL(strings.Replace(value, "%s", "test", 1)) {
		e.add(field, "Indirizzo https con %s al posto del testo cercato, es. https://www.google.com/search?q=%s")
	}
}
