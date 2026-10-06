package database

// Snapshot scrive in path una copia coerente del DB con VACUUM INTO (mai copiare
// il file a caldo: WAL e pagine in uso la renderebbero incoerente). Fallisce se
// path esiste già.
func (db *DB) Snapshot(path string) error {
	_, err := db.Exec(`VACUUM INTO ?`, path)
	return err
}

// CurrentSchemaVersion è la versione di schema prodotta dalle migrazioni di questo binario.
func CurrentSchemaVersion() int { return len(migrations) }
