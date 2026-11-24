package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// Registro delle modifiche dell'admin: requireAdmin scrive una riga per ogni
// POST riuscito, con sezione e azione da auditRoutes (chiave = r.Pattern) e ID
// e titolo passati dall'handler con auditObject. Una nuova route POST /admin/*
// va aggiunta qui o in auditSkip (TestAuditRoutesCover lo controlla).

type auditAction struct{ Section, Action string }

var auditRoutes = map[string]auditAction{
	"POST /admin/ente": {"Ente", "modificato"},

	"POST /admin/avvisi":                 {"Avvisi", "creato"},
	"POST /admin/avvisi/{id}":            {"Avvisi", "modificato"},
	"POST /admin/avvisi/{id}/elimina":    {"Avvisi", "eliminato"},
	"POST /admin/avvisi/{id}/archivia":   {"Avvisi", "archiviato"},
	"POST /admin/avvisi/{id}/ripristina": {"Avvisi", "ripristinato"},

	"POST /admin/calendario":              {"Calendario", "creato"},
	"POST /admin/calendario/{id}":         {"Calendario", "modificato"},
	"POST /admin/calendario/{id}/elimina": {"Calendario", "eliminato"},

	"POST /admin/app":              {"Applicativi", "creato"},
	"POST /admin/app/{id}":         {"Applicativi", "modificato"},
	"POST /admin/app/{id}/elimina": {"Applicativi", "eliminato"},
	"POST /admin/app/{id}/sposta":  {"Applicativi", "spostato"},

	"POST /admin/guide":               {"Guide", "creato"},
	"POST /admin/guide/{id}":          {"Guide", "modificato"},
	"POST /admin/guide/{id}/elimina":  {"Guide", "eliminato"},
	"POST /admin/guide/{id}/sposta":   {"Guide", "spostato"},
	"POST /admin/guide/{id}/aggiorna": {"Guide", "aggiornato"},

	"POST /admin/assistenza":              {"Assistenza", "creato"},
	"POST /admin/assistenza/{id}":         {"Assistenza", "modificato"},
	"POST /admin/assistenza/{id}/elimina": {"Assistenza", "eliminato"},
	"POST /admin/assistenza/{id}/sposta":  {"Assistenza", "spostato"},

	"POST /admin/categorie":              {"Categorie", "creato"},
	"POST /admin/categorie/{id}":         {"Categorie", "modificato"},
	"POST /admin/categorie/{id}/elimina": {"Categorie", "eliminato"},
	"POST /admin/categorie/{id}/sposta":  {"Categorie", "spostato"},

	"POST /admin/gruppi":                           {"Gruppi", "creato"},
	"POST /admin/gruppi/{id}":                      {"Gruppi", "rinominato"},
	"POST /admin/gruppi/{id}/elimina":              {"Gruppi", "eliminato"},
	"POST /admin/gruppi/{id}/sposta":               {"Gruppi", "spostato"},
	"POST /admin/gruppi/{id}/regole":               {"Gruppi", "regola aggiunta"},
	"POST /admin/gruppi/{id}/regole/{rid}/elimina": {"Gruppi", "regola eliminata"},
	"POST /admin/gruppi/attributi":                 {"Gruppi", "attributo aggiunto"},
	"POST /admin/gruppi/attributi/{id}/elimina":    {"Gruppi", "attributo eliminato"},
	"POST /admin/gruppi/attributi/{id}/testata":    {"Gruppi", "testata modificata"},
	"POST /admin/gruppi/attributi/{id}/sposta":     {"Gruppi", "testata spostata"},

	"POST /admin/backup":                {"Backup", "backup creato"},
	"POST /admin/backup/{name}/elimina": {"Backup", "backup eliminato"},
}

// auditSkip: route che non cambiano nulla, o pezzi di un'operazione registrata
// altrove. I due ripristini scrivono la loro riga da startRestore (il DB viene
// sostituito: vedi pendingAuditPath).
var auditSkip = map[string]bool{
	"POST /admin/anteprima":                true,
	"POST /admin/gruppi/{id}/anteprima":    true,
	"POST /admin/notifiche/prova":          true,
	"POST /admin/media":                    true,
	"POST /admin/media/{id}/pezzo":         true,
	"POST /admin/media/{id}/fine":          true,
	"POST /admin/backup/upload":            true,
	"POST /admin/backup/upload/{id}/chunk": true,
	"POST /admin/backup/upload/{id}/fine":  true,
	"POST /admin/backup/{name}/ripristina": true,
}

// auditRecord: ID e titolo dell'oggetto, scritti dall'handler.
type auditRecord struct{ ObjectID, Title string }

type auditKey struct{}

// auditObject: l'handler indica l'oggetto toccato. Per le eliminazioni va
// chiamata col titolo letto prima di cancellare. Senza record (route fuori da
// requireAdmin) non fa nulla.
func auditObject(r *http.Request, id any, title string) {
	rec, _ := r.Context().Value(auditKey{}).(*auditRecord)
	if rec == nil {
		return
	}
	rec.ObjectID = fmt.Sprint(id)
	rec.Title = title
}

func withAuditRecord(r *http.Request) (*http.Request, *auditRecord) {
	rec := &auditRecord{}
	return r.WithContext(context.WithValue(r.Context(), auditKey{}, rec)), rec
}

// audit scrive la riga se la richiesta è un POST riuscito su una route registrata.
func (s *Server) audit(r *http.Request, rec *auditRecord, vw *validationWriter) {
	if r.Method != http.MethodPost || auditSkip[r.Pattern] {
		return
	}
	act, ok := auditRoutes[r.Pattern]
	if !ok {
		slog.Warn("registro admin: route senza voce", "route", r.Pattern)
		return
	}
	status := vw.status
	if status == 0 {
		status = http.StatusOK
	}
	if vw.invalid || status >= 400 {
		return
	}
	if err := s.db.AddAdminLog(database.AdminLogEntry{
		At: s.now(), Username: s.currentAdmin(r), Section: act.Section, Action: act.Action,
		ObjectID: rec.ObjectID, Title: rec.Title,
	}); err != nil {
		slog.Error("registro admin", "err", err)
	}
}

// pendingAuditPath: file accanto al DB con la riga del ripristino in corso.
func pendingAuditPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "admin_log_pending.json")
}
