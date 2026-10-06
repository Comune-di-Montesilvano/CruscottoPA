package database

import "time"

// Branding è l'identità dell'ente mostrata nelle pagine. Una sola riga
// (id = 1), creata vuota dalla migrazione 4: stringa vuota = non impostato.
type Branding struct {
	EnteName  string
	LogoFile  string    // file in UPLOAD_DIR/branding, "" = nessun logo
	UpdatedAt time.Time // zero = mai modificato
	UpdatedBy string
}

func (db *DB) GetBranding() (Branding, error) {
	var b Branding
	var updated string
	err := db.QueryRow(`SELECT ente_name, logo_file, updated_at, updated_by FROM branding WHERE id = 1`).
		Scan(&b.EnteName, &b.LogoFile, &updated, &b.UpdatedBy)
	if err != nil || updated == "" {
		return b, err
	}
	b.UpdatedAt, err = parseTime(updated)
	return b, err
}

func (db *DB) UpdateBranding(b Branding) error {
	return checkAffected(db.Exec(
		`UPDATE branding SET ente_name = ?, logo_file = ?, updated_at = ?, updated_by = ? WHERE id = 1`,
		b.EnteName, b.LogoFile, formatTime(b.UpdatedAt), b.UpdatedBy))
}
