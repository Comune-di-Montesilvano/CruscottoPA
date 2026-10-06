package backup

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ChunkSize resta sotto il limite di default di 1 MB dei reverse proxy nginx.
const (
	ChunkSize = 512 << 10
	uploadTTL = time.Hour
)

var (
	ErrUploadNotFound = errors.New("Caricamento non trovato o scaduto: riprova.")
	ErrChunkOrder     = errors.New("Pezzo del file fuori sequenza.")
	ErrChunkTooBig    = errors.New("Pezzo del file troppo grande.")
)

type upload struct {
	f       *os.File
	path    string
	next    int
	size    int64
	updated time.Time
}

func (s *Service) StartUpload() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	path := filepath.Join(s.dir, "upload-"+id+".part")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return "", err
	}
	s.upMu.Lock()
	s.uploads[id] = &upload{f: f, path: path, updated: s.o.Now()}
	s.upMu.Unlock()
	return id, nil
}

func (s *Service) WriteChunk(id string, n int, r io.Reader) error {
	s.upMu.Lock()
	defer s.upMu.Unlock()
	u := s.uploads[id]
	if u == nil {
		return ErrUploadNotFound
	}
	if n != u.next {
		return ErrChunkOrder
	}
	data, err := io.ReadAll(io.LimitReader(r, ChunkSize+1))
	if err != nil {
		return err
	}
	if len(data) > ChunkSize {
		return ErrChunkTooBig
	}
	if u.size+int64(len(data)) > s.o.MaxExtract {
		return invalid("Archivio troppo grande (oltre %d MB).", s.o.MaxExtract>>20)
	}
	if _, err := u.f.Write(data); err != nil {
		return err
	}
	u.next++
	u.size += int64(len(data))
	u.updated = s.o.Now()
	return nil
}

// FinishUpload chiude la sessione e restituisce il percorso del file caricato.
func (s *Service) FinishUpload(id string) (string, error) {
	s.upMu.Lock()
	u := s.uploads[id]
	delete(s.uploads, id)
	s.upMu.Unlock()
	if u == nil {
		return "", ErrUploadNotFound
	}
	if err := u.f.Close(); err != nil {
		os.Remove(u.path)
		return "", err
	}
	return u.path, nil
}

func (s *Service) expireUploads() {
	s.upMu.Lock()
	defer s.upMu.Unlock()
	for id, u := range s.uploads {
		if s.o.Now().Sub(u.updated) > uploadTTL {
			u.f.Close()
			os.Remove(u.path)
			delete(s.uploads, id)
		}
	}
}
