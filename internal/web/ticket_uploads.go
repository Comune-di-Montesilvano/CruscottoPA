package web

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// Allegati dei ticket: caricati a pezzi come i media dell'admin, ma da
// utenti riconosciuti e legati allo username del cookie. Monouso: un id
// passa a OTRS una volta sola, poi il file si cancella.
const (
	ticketMaxFile    = 5 << 20
	ticketMaxFiles   = 3
	ticketMaxTotal   = 10 << 20
	ticketMaxPending = 100 // caricamenti aperti in tutto il server
	ticketMaxPerUser = 6   // caricamenti aperti per utente
	ticketUploadTTL  = time.Hour
)

var errTicketFiles = errors.New("allegati non validi")

type ticketUpload struct {
	username    string
	name        string
	path        string
	next        int
	size        int64
	done        bool
	writing     bool // un pezzo è in scrittura (fuori dal lock)
	taken       bool // in uso da un invio in corso
	contentType string
	started     time.Time
}

type ticketUploads struct {
	mu   sync.Mutex
	byID map[string]*ticketUpload
}

func (s *Server) ticketTmpDir() string { return filepath.Join(s.cfg.UploadDir, ".tmp", "ticket") }

// ticketUser: username del cookie, "" se anonimo o senza cookie.
func (s *Server) ticketUser(r *http.Request) string {
	u, ok := s.viewer(r)
	if !ok || u.Anonymous {
		return ""
	}
	return u.Username
}

// expireTicketUploads (con il lock): via i caricamenti più vecchi di un'ora.
func (s *Server) expireTicketUploads() {
	now := s.now()
	for id, u := range s.ticketFiles.byID {
		if now.Sub(u.started) > ticketUploadTTL {
			os.Remove(u.path)
			delete(s.ticketFiles.byID, id)
		}
	}
}

func (s *Server) handleTicketFileStart(w http.ResponseWriter, r *http.Request) {
	user := s.ticketUser(r)
	if user == "" {
		mediaFail(w, "Per allegare file devi essere riconosciuto.")
		return
	}
	name := r.FormValue("nome")
	if !utf8.ValidString(name) || len(name) > 255 {
		name = "allegato"
	}
	s.ticketFiles.mu.Lock()
	defer s.ticketFiles.mu.Unlock()
	s.expireTicketUploads()
	if len(s.ticketFiles.byID) >= ticketMaxPending {
		mediaFail(w, "Troppi caricamenti in corso, riprova fra poco.")
		return
	}
	mine := 0
	for _, u := range s.ticketFiles.byID {
		if u.username == user {
			mine++
		}
	}
	if mine >= ticketMaxPerUser {
		mediaFail(w, "Troppi allegati caricati: togline qualcuno o invia il ticket.")
		return
	}
	if err := os.MkdirAll(s.ticketTmpDir(), 0o750); err != nil {
		slog.Error("ticket: cartella temporanea", "err", err)
		mediaFail(w, "Caricamento non riuscito.")
		return
	}
	id := randomHex()
	s.ticketFiles.byID[id] = &ticketUpload{username: user, name: name, path: filepath.Join(s.ticketTmpDir(), id), started: s.now()}
	mediaJSON(w, map[string]any{"ok": true, "id": id, "chunk": mediaChunk})
}

// handleTicketFileChunk: il corpo si legge e si scrive fuori dal lock, così un
// client lento non ferma gli altri caricamenti (né gli invii con allegati).
func (s *Server) handleTicketFileChunk(w http.ResponseWriter, r *http.Request) {
	user := s.ticketUser(r)
	n, nerr := strconv.Atoi(r.URL.Query().Get("n"))
	id := r.PathValue("id")
	data, rerr := io.ReadAll(io.LimitReader(r.Body, mediaChunk+1))

	s.ticketFiles.mu.Lock()
	u := s.ticketFiles.byID[id]
	if u == nil || u.username != user || u.done || u.writing {
		s.ticketFiles.mu.Unlock()
		mediaFail(w, "Caricamento scaduto, riprova.")
		return
	}
	abort := func(msg string) { // con il lock preso
		delete(s.ticketFiles.byID, id)
		os.Remove(u.path)
		s.ticketFiles.mu.Unlock()
		mediaFail(w, msg)
	}
	if nerr != nil || n != u.next {
		abort("Caricamento interrotto, riprova.")
		return
	}
	if rerr != nil || len(data) > mediaChunk || u.size+int64(len(data)) > ticketMaxFile {
		abort("File troppo grande (massimo 5 MB).")
		return
	}
	u.writing = true
	s.ticketFiles.mu.Unlock()

	f, err := os.OpenFile(u.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err == nil {
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}

	s.ticketFiles.mu.Lock()
	u.writing = false
	if err != nil {
		slog.Error("ticket: scrittura pezzo", "err", err)
		abort("Caricamento non riuscito.")
		return
	}
	u.next++
	u.size += int64(len(data))
	s.ticketFiles.mu.Unlock()
	mediaJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleTicketFileFinish(w http.ResponseWriter, r *http.Request) {
	user := s.ticketUser(r)
	id := r.PathValue("id")
	s.ticketFiles.mu.Lock()
	defer s.ticketFiles.mu.Unlock()
	u := s.ticketFiles.byID[id]
	if u == nil || u.username != user || u.done || u.writing {
		mediaFail(w, "Caricamento scaduto, riprova.")
		return
	}
	head := make([]byte, 512)
	k := 0
	if f, err := os.Open(u.path); err == nil {
		k, _ = io.ReadFull(f, head)
		f.Close()
	}
	ct := ticketContentType(head[:k])
	if ct == "" {
		delete(s.ticketFiles.byID, id)
		os.Remove(u.path)
		mediaFail(w, "Formato non ammesso: usa PNG, JPEG, WebP o PDF.")
		return
	}
	u.done, u.contentType = true, ct
	mediaJSON(w, map[string]any{"ok": true, "id": id, "nome": u.name, "size": u.size})
}

// ticketContentType: tipo dal contenuto, mai dal nome. "" = non ammesso.
func ticketContentType(head []byte) string {
	if bytes.HasPrefix(head, []byte("%PDF-")) {
		return "application/pdf"
	}
	switch ct := http.DetectContentType(head); ct {
	case "image/png", "image/jpeg", "image/webp":
		return ct
	}
	return ""
}

// takeTicketFiles prende gli allegati di username per l'invio: tutti validi o
// nessuno. Restano in uso (taken) finché l'invio non finisce: riuscito →
// consumeTicketFiles (monouso), fallito → releaseTicketFiles (si può riprovare).
func (s *Server) takeTicketFiles(username string, ids []string) ([]otrs.Attachment, []string, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	if len(ids) > ticketMaxFiles {
		return nil, nil, errTicketFiles
	}
	s.ticketFiles.mu.Lock()
	ups := make([]*ticketUpload, 0, len(ids))
	var total int64
	seen := map[string]bool{}
	for _, id := range ids {
		u := s.ticketFiles.byID[id]
		if u == nil || seen[id] || !u.done || u.taken || u.username != username {
			s.ticketFiles.mu.Unlock()
			return nil, nil, errTicketFiles
		}
		seen[id] = true
		total += u.size
		ups = append(ups, u)
	}
	if total > ticketMaxTotal {
		s.ticketFiles.mu.Unlock()
		return nil, nil, errTicketFiles
	}
	for _, u := range ups {
		u.taken = true
	}
	s.ticketFiles.mu.Unlock()

	atts := make([]otrs.Attachment, 0, len(ups))
	paths := make([]string, 0, len(ups))
	for _, u := range ups {
		paths = append(paths, u.path)
	}
	for _, u := range ups {
		data, err := os.ReadFile(u.path)
		if err != nil {
			slog.Error("ticket: lettura allegato", "err", err)
			s.consumeTicketFiles(ids)
			return nil, nil, errTicketFiles
		}
		atts = append(atts, otrs.Attachment{Filename: u.name, ContentType: u.contentType, Content: data})
	}
	return atts, paths, nil
}

// consumeTicketFiles: invio riuscito, gli allegati non servono più.
func (s *Server) consumeTicketFiles(ids []string) {
	var paths []string
	s.ticketFiles.mu.Lock()
	for _, id := range ids {
		if u := s.ticketFiles.byID[id]; u != nil {
			paths = append(paths, u.path)
			delete(s.ticketFiles.byID, id)
		}
	}
	s.ticketFiles.mu.Unlock()
	s.removeTicketFiles(paths)
}

// releaseTicketFiles: invio fallito, gli allegati tornano disponibili.
func (s *Server) releaseTicketFiles(ids []string) {
	s.ticketFiles.mu.Lock()
	for _, id := range ids {
		if u := s.ticketFiles.byID[id]; u != nil {
			u.taken = false
		}
	}
	s.ticketFiles.mu.Unlock()
}

func (s *Server) removeTicketFiles(paths []string) {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("ticket: rimozione allegato", "err", err)
		}
	}
}
