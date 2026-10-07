package database

// MediaReferenced: il file caricato (nome casuale, senza percorso) è usato da
// un avviso, da una guida Markdown o come PDF di una guida. I nomi sono
// [0-9a-f]{32}.ext: niente caratteri speciali di LIKE da escapare.
func (db *DB) MediaReferenced(name string) (bool, error) {
	like := "%" + name + "%"
	var n int
	err := db.QueryRow(`SELECT
	(SELECT COUNT(*) FROM alerts WHERE body LIKE ?) +
	(SELECT COUNT(*) FROM guides WHERE (kind = 'markdown' AND body LIKE ?) OR file = ?)`,
		like, like, name).Scan(&n)
	return n > 0, err
}
