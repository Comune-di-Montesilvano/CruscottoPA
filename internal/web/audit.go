package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

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

// auditRecord: oggetto toccato. ObjectID e Title li scrive l'handler con
// auditObject (creazioni, Ente, backup); before è il titolo letto prima
// dell'handler, per le eliminazioni.
type auditRecord struct {
	set             bool
	ObjectID, Title string
	before          string
}

type auditKey struct{}

// auditObject: l'handler indica l'oggetto toccato quando la route non lo dice
// (creazioni: l'ID nuovo). Titolo vuoto = letto dal DB dopo l'handler. Senza
// record (route fuori da requireAdmin) non fa nulla.
func auditObject(r *http.Request, id any, title string) {
	rec, _ := r.Context().Value(auditKey{}).(*auditRecord)
	if rec == nil {
		return
	}
	rec.set = true
	rec.ObjectID = fmt.Sprint(id)
	rec.Title = title
}

// withAuditRecord prepara il record e legge il titolo dell'oggetto prima
// dell'handler (dopo un'eliminazione non ci sarebbe più).
func (s *Server) withAuditRecord(r *http.Request) (*http.Request, *auditRecord) {
	rec := &auditRecord{}
	if _, ok := auditRoutes[r.Pattern]; ok && r.Method == http.MethodPost {
		rec.before = s.auditTitle(r, auditPathID(r))
	}
	return r.WithContext(context.WithValue(r.Context(), auditKey{}, rec)), rec
}

// auditPathID: l'oggetto indicato dal percorso (la regola prima del gruppo).
func auditPathID(r *http.Request) string {
	for _, k := range []string{"rid", "id", "name"} {
		if v := r.PathValue(k); v != "" {
			return v
		}
	}
	return ""
}

// auditTitle legge il titolo dell'oggetto id per la route della richiesta;
// "" se non esiste o non ha un titolo (backup).
func (s *Server) auditTitle(r *http.Request, id string) string {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return ""
	}
	p := r.Pattern
	switch {
	case strings.Contains(p, "/regole"):
		gid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			return ""
		}
		g, err := s.db.GetAudienceGroup(gid)
		if err != nil {
			return ""
		}
		rules, err := s.db.ListAudienceRules(gid)
		if err != nil {
			return ""
		}
		for _, ru := range rules {
			if ru.ID == n {
				return g.Name + ": " + auditRuleText(ru)
			}
		}
	case strings.Contains(p, "/attributi"):
		attrs, err := s.db.ListAudienceAttributes()
		if err != nil {
			return ""
		}
		for _, a := range attrs {
			if a.ID == n {
				return a.Name
			}
		}
	default:
		return s.auditSectionTitle(auditRoutes[p].Section, n)
	}
	return ""
}

func (s *Server) auditSectionTitle(section string, id int64) string {
	var title string
	var err error
	switch section {
	case "Avvisi":
		var a database.Alert
		a, err = s.db.GetAlert(id)
		title = a.Title
	case "Calendario":
		var e database.CalendarEvent
		e, err = s.db.GetCalendarEvent(id)
		title = e.Title
	case "Applicativi":
		var a database.App
		a, err = s.db.GetApp(id)
		title = a.Title
	case "Guide":
		var g database.Guide
		g, err = s.db.GetGuide(id)
		title = g.Title
	case "Assistenza":
		var c database.SupportChannel
		c, err = s.db.GetSupportChannel(id)
		title = c.Title
	case "Categorie":
		var c database.Category
		c, err = s.db.GetCategory(id)
		title = c.Name
	case "Gruppi":
		var g database.AudienceGroup
		g, err = s.db.GetAudienceGroup(id)
		title = g.Name
	}
	if err != nil {
		return ""
	}
	return title
}

// auditRuleText: la regola in breve, es. "department = TRIBUTI" o "user mrossi".
func auditRuleText(r database.AudienceRule) string {
	if r.Attr != "" && r.Value != "" {
		return r.Attr + " = " + r.Value
	}
	if r.Attr != "" {
		return r.Kind + " " + r.Attr
	}
	return r.Kind + " " + r.Value
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
	id, title := auditPathID(r), ""
	if rec.set {
		id, title = rec.ObjectID, rec.Title
	}
	if title == "" {
		title = s.auditTitle(r, id) // dopo l'handler: il titolo nuovo
	}
	if title == "" {
		title = rec.before // eliminazioni
	}
	if err := s.db.AddAdminLog(database.AdminLogEntry{
		At: s.now(), Username: s.currentAdmin(r), Section: act.Section, Action: act.Action,
		ObjectID: id, Title: title,
	}); err != nil {
		slog.Error("registro admin", "err", err)
	}
}

// pendingAuditPath: file accanto al DB con la riga del ripristino in corso.
func pendingAuditPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "admin_log_pending.json")
}
