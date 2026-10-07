package web

import (
	"fmt"
	"net/http"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/markdown"
)

// handlePreview: lo stesso rendering della plancia, per l'anteprima dell'editor.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<div class="md preview">%s</div>`, markdown.Render(r.FormValue("body"), markdown.Options{}))
}
