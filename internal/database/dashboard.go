package database

import "time"

type AppWithGuides struct {
	App
	Guides []Guide
}

type CategoryWithApps struct {
	Category
	Apps []AppWithGuides
}

// Dashboard contiene tutto ciò che serve alla plancia. Slice mai nil.
type Dashboard struct {
	Alerts        []Alert
	Categories    []CategoryWithApps
	GeneralGuides []Guide
}

// Un'app è visibile se abilitata e con URL; una guida se abilitata e generale
// oppure agganciata a un'app visibile.
const visibleApp = `a.enabled = 1 AND a.url <> ''`

func (db *DB) GetDashboard(now time.Time) (Dashboard, error) {
	d := Dashboard{Categories: []CategoryWithApps{}, GeneralGuides: []Guide{}}

	var err error
	if d.Alerts, err = db.ListActiveAlerts(now); err != nil {
		return d, err
	}

	rows, err := db.Query(`SELECT c.id, c.name, c.sort_order, ` + appCols + `
FROM apps a JOIN categories c ON c.id = a.category_id
WHERE ` + visibleApp + `
ORDER BY c.sort_order, c.name COLLATE NOCASE, ` + appOrder)
	if err != nil {
		return d, err
	}
	index := map[int64]appPos{}
	for rows.Next() {
		var c Category
		a, err := scanApp(rows, &c.ID, &c.Name, &c.SortOrder)
		if err != nil {
			rows.Close()
			return d, err
		}
		last := len(d.Categories) - 1
		if last < 0 || d.Categories[last].ID != c.ID {
			d.Categories = append(d.Categories, CategoryWithApps{Category: c})
			last++
		}
		d.Categories[last].Apps = append(d.Categories[last].Apps, AppWithGuides{App: a, Guides: []Guide{}})
		index[a.ID] = appPos{last, len(d.Categories[last].Apps) - 1}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return d, err
	}

	guides, err := queryGuides(db, `SELECT `+guideCols+` FROM guides g
LEFT JOIN apps a ON a.id = g.app_id
WHERE g.enabled = 1 AND (g.app_id IS NULL OR (`+visibleApp+`))
ORDER BY `+guideOrder)
	if err != nil {
		return d, err
	}
	attachGuides(&d, index, guides)
	return d, nil
}

// appPos è la posizione di un'app visibile in Dashboard.Categories.
type appPos struct{ cat, app int }

// attachGuides distribuisce le guide: generali nella colonna, le altre sulla tile della loro app.
func attachGuides(d *Dashboard, index map[int64]appPos, guides []Guide) {
	for _, g := range guides {
		if g.AppID == nil {
			d.GeneralGuides = append(d.GeneralGuides, g)
			continue
		}
		p, ok := index[*g.AppID]
		if !ok {
			continue // app non (più) visibile in questa lettura
		}
		d.Categories[p.cat].Apps[p.app].Guides = append(d.Categories[p.cat].Apps[p.app].Guides, g)
	}
}
