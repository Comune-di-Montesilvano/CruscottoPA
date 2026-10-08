package database

import "time"

// Branding è l'identità dell'ente mostrata nelle pagine. Una sola riga
// (id = 1), creata vuota dalla migrazione 4: stringa vuota = non impostato.
type Branding struct {
	EnteName  string
	LogoFile  string    // file in UPLOAD_DIR/branding, "" = nessun logo
	UpdatedAt time.Time // zero = mai modificato
	UpdatedBy string
	// Ricerca dalla barra della plancia: URL con %s al posto del testo.
	WebSearch  string // motore web (predefinito Google)
	SiteSearch string // ricerca del sito dell'ente, "" = nessuna
}

// DefaultWebSearch: motore web predefinito della plancia.
const DefaultWebSearch = "https://www.google.com/search?q=%s"

func (db *DB) GetBranding() (Branding, error) {
	var b Branding
	var updated string
	err := db.QueryRow(`SELECT ente_name, logo_file, updated_at, updated_by, web_search, site_search FROM branding WHERE id = 1`).
		Scan(&b.EnteName, &b.LogoFile, &updated, &b.UpdatedBy, &b.WebSearch, &b.SiteSearch)
	if err != nil || updated == "" {
		return b, err
	}
	b.UpdatedAt, err = parseTime(updated)
	return b, err
}

func (db *DB) UpdateBranding(b Branding) error {
	return checkAffected(db.Exec(
		`UPDATE branding SET ente_name = ?, logo_file = ?, updated_at = ?, updated_by = ?, web_search = ?, site_search = ? WHERE id = 1`,
		b.EnteName, b.LogoFile, formatTime(b.UpdatedAt), b.UpdatedBy, b.WebSearch, b.SiteSearch))
}
