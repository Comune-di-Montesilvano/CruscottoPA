package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// lastAudit verifica l'ultima riga del registro.
func lastAudit(t *testing.T, db *database.DB, section, action, objectID, title string) {
	t.Helper()
	rows := auditRows(t, db)
	if len(rows) == 0 {
		t.Fatalf("%s %s: registro vuoto", section, action)
	}
	r := rows[0]
	if r.Section != section || r.Action != action || r.ObjectID != objectID || r.Title != title {
		t.Fatalf("atteso %s/%s/%s/%q, ottenuto %+v", section, action, objectID, title, r)
	}
}

type idTitle struct {
	id    int64
	title string
}

func TestAuditTitles(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	post := func(path string, f url.Values) {
		t.Helper()
		if rec := do(t, s, "POST", path, f, c, hx); rec.Code != 200 || invalid(rec) {
			t.Fatalf("%s: %d\n%s", path, rec.Code, rec.Body)
		}
	}
	cases := []struct {
		section, base, edit string
		form                func(title string) url.Values
		list                func() []idTitle
	}{
		{"Avvisi", "/admin/avvisi", "modificato",
			func(title string) url.Values {
				return url.Values{"title": {title}, "level": {"news"}, "starts_at": {"2026-10-06T09:00"}}
			},
			func() []idTitle {
				cur, exp, _ := db.ListAlertsForAdmin(fixedNow)
				var out []idTitle
				for _, a := range append(cur, exp...) {
					out = append(out, idTitle{a.ID, a.Title})
				}
				return out
			}},
		{"Calendario", "/admin/calendario", "modificato",
			func(title string) url.Values { return calForm(title, "closure", "2026-10-31", "") },
			func() []idTitle {
				evs, _ := db.ListCalendarEvents()
				var out []idTitle
				for _, e := range evs {
					out = append(out, idTitle{e.ID, e.Title})
				}
				return out
			}},
		{"Guide", "/admin/guide", "modificato",
			func(title string) url.Values {
				return url.Values{"title": {title}, "url": {"https://wiki.example.it/vpn"}, "app_id": {"0"}, "enabled": {"1"}}
			},
			func() []idTitle {
				gs, _ := db.ListGuides()
				var out []idTitle
				for _, g := range gs {
					out = append(out, idTitle{g.ID, g.Title})
				}
				return out
			}},
		{"Assistenza", "/admin/assistenza", "modificato",
			func(title string) url.Values {
				return url.Values{"title": {title}, "url": {"https://assistenza.example.it"}}
			},
			func() []idTitle {
				cs, _ := db.ListSupportChannels()
				var out []idTitle
				for _, ch := range cs {
					out = append(out, idTitle{ch.ID, ch.Title})
				}
				return out
			}},
		{"Categorie", "/admin/categorie", "modificato",
			func(title string) url.Values { return url.Values{"name": {title}} },
			func() []idTitle {
				cs, _ := db.ListCategories()
				var out []idTitle
				for _, ct := range cs {
					out = append(out, idTitle{ct.ID, ct.Name})
				}
				return out
			}},
		{"Gruppi", "/admin/gruppi", "rinominato",
			func(title string) url.Values { return url.Values{"name": {title}} },
			func() []idTitle {
				gs, _ := db.ListAudienceGroups()
				var out []idTitle
				for _, g := range gs {
					out = append(out, idTitle{g.ID, g.Name})
				}
				return out
			}},
	}
	for _, tc := range cases {
		t.Run(tc.section, func(t *testing.T) {
			post(tc.base, tc.form("Prima"))
			id := ""
			for _, it := range tc.list() {
				if it.title == "Prima" {
					id = itoa(it.id)
				}
			}
			if id == "" {
				t.Fatal("oggetto creato non trovato")
			}
			lastAudit(t, db, tc.section, "creato", id, "Prima")
			post(tc.base+"/"+id, tc.form("Seconda"))
			lastAudit(t, db, tc.section, tc.edit, id, "Seconda")
			post(tc.base+"/"+id+"/elimina", nil)
			lastAudit(t, db, tc.section, "eliminato", id, "Seconda")
		})
	}

	t.Run("Applicativi", func(t *testing.T) {
		if rec := postMultipart(t, s, "/admin/app", appFields(db, map[string]string{"title": "Protocollo"}), nil, c); rec.Code != 200 || invalid(rec) {
			t.Fatalf("crea: %d", rec.Code)
		}
		apps, _ := db.ListApps()
		id := ""
		for _, a := range apps {
			if a.Title == "Protocollo" {
				id = itoa(a.ID)
			}
		}
		lastAudit(t, db, "Applicativi", "creato", id, "Protocollo")
		postMultipart(t, s, "/admin/app/"+id, appFields(db, map[string]string{"title": "Protocollo web"}), nil, c)
		lastAudit(t, db, "Applicativi", "modificato", id, "Protocollo web")
		post("/admin/app/"+id+"/sposta", url.Values{"dir": {"up"}})
		lastAudit(t, db, "Applicativi", "spostato", id, "Protocollo web")
		post("/admin/app/"+id+"/elimina", nil)
		lastAudit(t, db, "Applicativi", "eliminato", id, "Protocollo web")
	})

	t.Run("Attributi e regole", func(t *testing.T) {
		post("/admin/gruppi/attributi", url.Values{"name": {"department"}, "label": {"Ufficio"}})
		attrs, _ := db.ListAudienceAttributes()
		aid := ""
		for _, a := range attrs {
			if a.Name == "department" {
				aid = itoa(a.ID)
			}
		}
		lastAudit(t, db, "Gruppi", "attributo aggiunto", aid, "department")
		post("/admin/gruppi", url.Values{"name": {"Tributi"}})
		var gid int64
		gs, _ := db.ListAudienceGroups()
		for _, g := range gs {
			if g.Name == "Tributi" {
				gid = g.ID
			}
		}
		post("/admin/gruppi/"+itoa(gid)+"/regole", url.Values{"kind": {"attr"}, "attr": {"department"}, "value": {"TRIBUTI"}})
		rules, _ := db.ListAudienceRules(gid)
		rid := itoa(rules[0].ID)
		lastAudit(t, db, "Gruppi", "regola aggiunta", rid, "Tributi: department = TRIBUTI")
		post("/admin/gruppi/"+itoa(gid)+"/regole/"+rid+"/elimina", nil)
		lastAudit(t, db, "Gruppi", "regola eliminata", rid, "Tributi: department = TRIBUTI")
		post("/admin/gruppi/attributi/"+aid+"/elimina", nil)
		lastAudit(t, db, "Gruppi", "attributo eliminato", aid, "department")
	})

	t.Run("Ente", func(t *testing.T) {
		postEnte(t, s, map[string]string{"ente_name": "Comune di Prova"}, nil, c)
		lastAudit(t, db, "Ente", "modificato", "", "Comune di Prova")
	})

	t.Run("Backup", func(t *testing.T) {
		post("/admin/backup", nil)
		rows := auditRows(t, db)
		if rows[0].Action != "backup creato" || !strings.HasSuffix(rows[0].ObjectID, ".tar.gz") {
			t.Fatalf("riga: %+v", rows[0])
		}
		name := rows[0].ObjectID
		post("/admin/backup/"+name+"/elimina", nil)
		lastAudit(t, db, "Backup", "backup eliminato", name, "")
	})
}

// Un attributo o un gruppo in uso non si elimina: la risposta mostra l'errore
// e il registro non deve contare l'eliminazione.
func TestAuditInUseNotLogged(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	do(t, s, "POST", "/admin/gruppi/attributi", url.Values{"name": {"department"}, "label": {"Ufficio"}}, c, hx)
	do(t, s, "POST", "/admin/gruppi", url.Values{"name": {"Tributi"}}, c, hx)
	attrs, _ := db.ListAudienceAttributes()
	var aid, gid string
	for _, a := range attrs {
		if a.Name == "department" {
			aid = itoa(a.ID)
		}
	}
	gs, _ := db.ListAudienceGroups()
	for _, g := range gs {
		if g.Name == "Tributi" {
			gid = itoa(g.ID)
		}
	}
	do(t, s, "POST", "/admin/gruppi/"+gid+"/regole", url.Values{"kind": {"attr"}, "attr": {"department"}, "value": {"TRIBUTI"}}, c, hx)
	rec := do(t, s, "POST", "/admin/gruppi/attributi/"+aid+"/elimina", nil, c, hx)
	if !strings.Contains(rec.Body.String(), "Attributo usato da qualche regola") {
		t.Fatalf("errore non mostrato:\n%s", rec.Body)
	}
	for _, r := range auditRows(t, db) {
		if r.Action == "attributo eliminato" {
			t.Fatalf("eliminazione dell'attributo registrata: %+v", r)
		}
	}
	do(t, s, "POST", "/admin/avvisi", url.Values{"title": {"Solo tributi"}, "level": {"news"}, "starts_at": {"2026-10-06T09:00"},
		"visibilita": {"only"}, "gruppi": {gid}}, c, hx)
	do(t, s, "POST", "/admin/gruppi/"+gid+"/elimina", nil, c, hx)
	for _, r := range auditRows(t, db) {
		if r.Section == "Gruppi" && r.Action == "eliminato" {
			t.Fatalf("eliminazione del gruppo registrata: %+v", r)
		}
	}
}
