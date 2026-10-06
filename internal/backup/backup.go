// Package backup crea, elenca, conserva e ripristina archivi tar.gz con lo
// snapshot del DB SQLite e la cartella degli upload.
package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	KindAuto       = "auto"
	KindManual     = "manuale"
	KindPreRestore = "pre-ripristino"

	formatVersion  = 1
	nameTimeLayout = "20060102-150405"
	manifestEntry  = "manifest.json"
	dbEntry        = "cruscotto.db"
	uploadsEntry   = "uploads"
)

var (
	ErrBusy      = errors.New("Operazione di backup o ripristino già in corso: riprova tra poco.")
	ErrNotFound  = errors.New("Backup non trovato.")
	ErrProtected = errors.New("I backup automatici sono gestiti dalla conservazione automatica e non si eliminano a mano.")

	nameRe = regexp.MustCompile(`^cruscotto-(\d{8}-\d{6})-(auto|manuale|pre-ripristino)\.tar\.gz$`)
)

// Store è ciò che il servizio usa del DB (implementato da *database.DB).
type Store interface {
	Snapshot(path string) error
	SchemaVersion() (int, error)
	Close() error
}

// Manifest descrive un archivio; è la prima voce del tar.
type Manifest struct {
	Format        int       `json:"format"`
	AppVersion    string    `json:"app_version"`
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
	Kind          string    `json:"kind"`
	Files         int       `json:"files"`
}

// Info è un archivio presente nella cartella dei backup.
type Info struct {
	Name      string
	Kind      string
	CreatedAt time.Time
	Size      int64
}

// Status riassume l'esito dei backup per la panoramica admin.
type Status struct {
	LastSuccess   time.Time // zero se non c'è nessun backup
	LastAutoError string    // "" se l'ultimo automatico è riuscito
}

type Options struct {
	Store      Store
	DBPath     string
	UploadDir  string
	AppVersion string
	MaxSchema  int
	Location   *time.Location
	Now        func() time.Time
	Exit       func(code int)
	MaxExtract int64 // limite archivio caricato e contenuto estratto
}

type Service struct {
	o   Options
	dir string

	mu sync.Mutex // un backup/ripristino alla volta

	stMu        sync.Mutex
	lastOK      time.Time
	lastAutoErr string

	upMu    sync.Mutex
	uploads map[string]*upload
}

func New(o Options) (*Service, error) {
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Exit == nil {
		o.Exit = os.Exit
	}
	if o.MaxExtract == 0 {
		o.MaxExtract = 2 << 30
	}
	s := &Service{o: o, dir: filepath.Join(filepath.Dir(o.DBPath), "backups"), uploads: map[string]*upload{}}
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return nil, fmt.Errorf("cartella backup: %w", err)
	}
	s.cleanup()
	return s, nil
}

func (s *Service) restoreDir() string { return filepath.Join(filepath.Dir(s.o.DBPath), "restore-tmp") }

// cleanup rimuove i residui di operazioni interrotte da un crash o da un riavvio.
func (s *Service) cleanup() {
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".tmp") || strings.HasSuffix(n, ".part") {
			os.Remove(filepath.Join(s.dir, n))
		}
	}
	os.RemoveAll(s.restoreDir())
	os.RemoveAll(s.o.UploadDir + ".old")
}

// Create crea un backup del tipo indicato (KindAuto o KindManual).
func (s *Service) Create(kind string) (Info, error) {
	if !s.mu.TryLock() {
		return Info{}, ErrBusy
	}
	defer s.mu.Unlock()
	info, err := s.create(kind)
	s.record(kind, err)
	return info, err
}

func (s *Service) record(kind string, err error) {
	s.stMu.Lock()
	defer s.stMu.Unlock()
	if err == nil {
		s.lastOK = s.o.Now()
		if kind == KindAuto {
			s.lastAutoErr = ""
		}
		return
	}
	if kind == KindAuto {
		s.lastAutoErr = err.Error()
	}
}

func (s *Service) create(kind string) (Info, error) {
	now := s.o.Now()
	name := fmt.Sprintf("cruscotto-%s-%s.tar.gz", now.In(s.o.Location).Format(nameTimeLayout), kind)
	final := filepath.Join(s.dir, name)
	if _, err := os.Stat(final); err == nil {
		return Info{}, fmt.Errorf("esiste già un backup con nome %s: riprova tra un secondo", name)
	}

	snap := final + ".db.tmp"
	defer os.Remove(snap)
	if err := s.o.Store.Snapshot(snap); err != nil {
		return Info{}, fmt.Errorf("copia del database: %w", err)
	}
	schema, err := s.o.Store.SchemaVersion()
	if err != nil {
		return Info{}, err
	}
	files, err := listFiles(s.o.UploadDir)
	if err != nil {
		return Info{}, fmt.Errorf("elenco upload: %w", err)
	}

	tmp := final + ".tmp"
	m := Manifest{Format: formatVersion, AppVersion: s.o.AppVersion, SchemaVersion: schema,
		CreatedAt: now.UTC(), Kind: kind, Files: len(files)}
	if err := writeArchive(tmp, m, snap, s.o.UploadDir, files); err != nil {
		os.Remove(tmp)
		return Info{}, fmt.Errorf("scrittura archivio: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return Info{}, err
	}
	st, err := os.Stat(final)
	if err != nil {
		return Info{}, err
	}
	return Info{Name: name, Kind: kind, CreatedAt: now, Size: st.Size()}, nil
}

// List restituisce gli archivi presenti, più recenti prima.
func (s *Service) List() ([]Info, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := []Info{}
	for _, e := range entries {
		m := nameRe.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		t, err := time.ParseInLocation(nameTimeLayout, m[1], s.o.Location)
		if err != nil {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Info{Name: e.Name(), Kind: m[2], CreatedAt: t, Size: fi.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Path restituisce il percorso di un archivio esistente; il nome è validato (niente traversal).
func (s *Service) Path(name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", ErrNotFound
	}
	p := filepath.Join(s.dir, name)
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return p, nil
}

func (s *Service) Delete(name string) error {
	m := nameRe.FindStringSubmatch(name)
	if m == nil {
		return ErrNotFound
	}
	if m[2] == KindAuto {
		return ErrProtected
	}
	p, err := s.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

func (s *Service) Status() Status {
	s.stMu.Lock()
	st := Status{LastSuccess: s.lastOK, LastAutoError: s.lastAutoErr}
	s.stMu.Unlock()
	if list, err := s.List(); err == nil && len(list) > 0 && list[0].CreatedAt.After(st.LastSuccess) {
		st.LastSuccess = list[0].CreatedAt
	}
	return st
}

// upload è una sessione di caricamento a pezzi (vedi upload.go).
type upload struct{}
