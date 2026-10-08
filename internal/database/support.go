package database

import (
	"database/sql"
	"strings"
)

// SupportChannel: dove chiedere aiuto per uno o più applicativi (es. portale
// di assistenza del fornitore). AppIDs mai nil.
type SupportChannel struct {
	ID        int64
	Title     string
	URL       string
	Note      string
	SortOrder int
	Enabled   bool
	AppIDs    []int64
}

const supportOrder = `sort_order, title COLLATE NOCASE, id`

func (db *DB) ListSupportChannels() ([]SupportChannel, error) {
	rows, err := db.Query(`SELECT id, title, url, note, sort_order, enabled FROM support_channels ORDER BY ` + supportOrder)
	if err != nil {
		return nil, err
	}
	out := []SupportChannel{}
	idx := map[int64]int{}
	for rows.Next() {
		c := SupportChannel{AppIDs: []int64{}}
		if err := rows.Scan(&c.ID, &c.Title, &c.URL, &c.Note, &c.SortOrder, &c.Enabled); err != nil {
			rows.Close()
			return nil, err
		}
		idx[c.ID] = len(out)
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Collegamenti nell'ordine della plancia (categoria, poi app).
	links, err := db.Query(`SELECT s.channel_id, s.app_id FROM app_support s
JOIN apps a ON a.id = s.app_id JOIN categories c ON c.id = a.category_id
ORDER BY c.sort_order, c.name COLLATE NOCASE, ` + appOrder)
	if err != nil {
		return nil, err
	}
	defer links.Close()
	for links.Next() {
		var ch, app int64
		if err := links.Scan(&ch, &app); err != nil {
			return nil, err
		}
		if i, ok := idx[ch]; ok {
			out[i].AppIDs = append(out[i].AppIDs, app)
		}
	}
	return out, links.Err()
}

func (db *DB) GetSupportChannel(id int64) (SupportChannel, error) {
	all, err := db.ListSupportChannels()
	if err != nil {
		return SupportChannel{}, err
	}
	for _, c := range all {
		if c.ID == id {
			return c, nil
		}
	}
	return SupportChannel{}, ErrNotFound
}

func (db *DB) CreateSupportChannel(c SupportChannel) (id int64, err error) {
	err = db.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO support_channels (title, url, note, enabled, sort_order)
VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM support_channels))`,
			strings.TrimSpace(c.Title), strings.TrimSpace(c.URL), strings.TrimSpace(c.Note), c.Enabled)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return setSupportApps(tx, id, c.AppIDs)
	})
	return id, err
}

func (db *DB) UpdateSupportChannel(c SupportChannel) error {
	return db.inTx(func(tx *sql.Tx) error {
		if err := checkAffected(tx.Exec(`UPDATE support_channels SET title = ?, url = ?, note = ?, enabled = ? WHERE id = ?`,
			strings.TrimSpace(c.Title), strings.TrimSpace(c.URL), strings.TrimSpace(c.Note), c.Enabled, c.ID)); err != nil {
			return err
		}
		return setSupportApps(tx, c.ID, c.AppIDs)
	})
}

func setSupportApps(tx *sql.Tx, channel int64, apps []int64) error {
	if _, err := tx.Exec(`DELETE FROM app_support WHERE channel_id = ?`, channel); err != nil {
		return err
	}
	for _, a := range apps {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO app_support (app_id, channel_id) VALUES (?, ?)`, a, channel); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) DeleteSupportChannel(id int64) error {
	return checkAffected(db.Exec(`DELETE FROM support_channels WHERE id = ?`, id))
}

func (db *DB) MoveSupportChannel(id int64, dir int) error {
	return db.moveRow("support_channels", "1 = 1", nil, supportOrder, id, dir)
}

// SupportByApp: canali attivi per applicativo, nell'ordine dell'admin.
func (db *DB) SupportByApp() (map[int64][]SupportChannel, error) {
	all, err := db.ListSupportChannels()
	if err != nil {
		return nil, err
	}
	out := map[int64][]SupportChannel{}
	for _, c := range all {
		if !c.Enabled {
			continue
		}
		for _, a := range c.AppIDs {
			out[a] = append(out[a], c)
		}
	}
	return out, nil
}
