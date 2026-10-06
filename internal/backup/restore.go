package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// ValidationError indica un archivio rifiutato; Msg è pensato per l'utente.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(format, a...)} }

var sqliteMagic = []byte("SQLite format 3\x00")

// extract valida l'archivio e lo estrae in restoreDir. Nessun file viene
// scritto fuori da restoreDir; in caso di errore restoreDir viene rimossa.
func (s *Service) extract(archive string) (m Manifest, err error) {
	dest := s.restoreDir()
	os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return m, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dest)
		}
	}()

	f, err := os.Open(archive)
	if err != nil {
		return m, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return m, invalid("Il file non è un archivio di backup valido (.tar.gz).")
	}
	tr := tar.NewReader(gz)

	var manifest *Manifest
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return m, invalid("Il file non è un archivio di backup valido (.tar.gz).")
		}
		name, err := entryName(hdr)
		if err != nil {
			return m, err
		}
		target, err := safeJoin(dest, name)
		if err != nil {
			return m, err
		}
		switch {
		case hdr.Typeflag == tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return m, err
			}
		case name == manifestEntry:
			var mm Manifest
			if err := json.NewDecoder(io.LimitReader(tr, 64<<10)).Decode(&mm); err != nil {
				return m, invalid("manifest.json non leggibile.")
			}
			manifest = &mm
		default:
			total += hdr.Size
			if total > s.o.MaxExtract {
				return m, invalid("Archivio troppo grande una volta estratto (oltre %d MB).", s.o.MaxExtract>>20)
			}
			if err := writeEntry(target, tr, hdr.Size); err != nil {
				return m, err
			}
		}
	}

	if manifest == nil {
		return m, invalid("Archivio senza manifest.json: non è un backup di CruscottoPA.")
	}
	if manifest.Format != formatVersion {
		return m, invalid("Formato di backup %d non supportato.", manifest.Format)
	}
	if manifest.SchemaVersion > s.o.MaxSchema {
		return m, invalid("Backup creato da una versione più recente di CruscottoPA (schema %d, supportato fino a %d): aggiorna prima l'applicazione.",
			manifest.SchemaVersion, s.o.MaxSchema)
	}
	dbPath := filepath.Join(dest, dbEntry)
	if _, err := os.Stat(dbPath); err != nil {
		return m, invalid("Archivio senza cruscotto.db.")
	}
	if err := checkDB(dbPath); err != nil {
		return m, err
	}
	return *manifest, nil
}

// safeJoin unisce dest e il nome di una voce garantendo che il risultato resti
// dentro dest (difesa in profondità oltre a entryName, contro lo "zip slip").
func safeJoin(dest, name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
		return "", invalid("Percorso non ammesso nell'archivio: %s", name)
	}
	base := filepath.Clean(dest)
	target := filepath.Join(base, filepath.FromSlash(name))
	if !strings.HasPrefix(target, base+string(os.PathSeparator)) {
		return "", invalid("Percorso non ammesso nell'archivio: %s", name)
	}
	return target, nil
}

// entryName ammette solo file/cartelle regolari sotto manifest.json, cruscotto.db, uploads/.
func entryName(hdr *tar.Header) (string, error) {
	name := hdr.Name
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || filepath.IsAbs(name) || strings.Contains(name, ":") {
		return "", invalid("Percorso non ammesso nell'archivio: %s", name)
	}
	clean := strings.TrimSuffix(name, "/")
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." {
			return "", invalid("Percorso non ammesso nell'archivio: %s", name)
		}
	}
	if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeDir {
		return "", invalid("Tipo di voce non ammesso nell'archivio: %s", name)
	}
	if clean != manifestEntry && clean != dbEntry && clean != uploadsEntry && !strings.HasPrefix(clean, uploadsEntry+"/") {
		return "", invalid("Voce inattesa nell'archivio: %s", name)
	}
	return clean, nil
}

func writeEntry(target string, r io.Reader, size int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return invalid("Voce duplicata nell'archivio: %s", filepath.Base(target))
	}
	_, err = io.CopyN(f, r, size)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return invalid("Archivio troncato.")
	}
	return err
}

// checkDB verifica header SQLite e integrità del file estratto.
func checkDB(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	head := make([]byte, len(sqliteMagic))
	_, err = io.ReadFull(f, head)
	f.Close()
	if err != nil || !bytes.Equal(head, sqliteMagic) {
		return invalid("Il database nell'archivio è danneggiato.")
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return invalid("Il database nell'archivio è danneggiato.")
	}
	defer db.Close()
	var res string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		return invalid("Il database nell'archivio è danneggiato.")
	}
	return nil
}

// PrepareRestore valida l'archivio e crea il backup pre-ripristino. Se tutto va
// a buon fine restituisce la funzione di swap, da eseguire dopo aver risposto al
// client: sostituisce DB e uploads e termina il processo (il container viene
// riavviato dalla restart policy). Il lock resta preso fino all'uscita.
func (s *Service) PrepareRestore(archive string, removeArchive bool) (func(), error) {
	if !s.mu.TryLock() {
		return nil, ErrBusy
	}
	_, err := s.extract(archive)
	if removeArchive {
		os.Remove(archive)
	}
	if err == nil {
		_, err = s.create(KindPreRestore)
		s.record(KindPreRestore, err)
		if err != nil {
			err = fmt.Errorf("backup pre-ripristino non riuscito: %w", err)
		}
	}
	if err != nil {
		os.RemoveAll(s.restoreDir())
		s.mu.Unlock()
		return nil, err
	}
	return s.swap, nil
}

func (s *Service) swap() {
	src := s.restoreDir()
	if err := s.o.Store.Close(); err != nil {
		slog.Error("ripristino: chiusura DB", "err", err)
	}
	os.Remove(s.o.DBPath + "-wal")
	os.Remove(s.o.DBPath + "-shm")
	if err := os.Rename(filepath.Join(src, dbEntry), s.o.DBPath); err != nil {
		slog.Error("ripristino: sostituzione DB non riuscita, usare il backup pre-ripristino", "err", err)
		s.o.Exit(1)
		return
	}
	old := s.o.UploadDir + ".old"
	os.RemoveAll(old)
	if _, err := os.Stat(s.o.UploadDir); err == nil {
		if err := os.Rename(s.o.UploadDir, old); err != nil {
			slog.Error("ripristino: spostamento uploads", "err", err)
			s.o.Exit(1)
			return
		}
	}
	if _, err := os.Stat(filepath.Join(src, uploadsEntry)); err == nil {
		if err := os.Rename(filepath.Join(src, uploadsEntry), s.o.UploadDir); err != nil {
			slog.Error("ripristino: sostituzione uploads", "err", err)
			s.o.Exit(1)
			return
		}
	} else {
		os.MkdirAll(s.o.UploadDir, 0o750)
	}
	os.RemoveAll(old)
	os.RemoveAll(src)
	slog.Info("ripristino completato: riavvio")
	s.o.Exit(0)
}
