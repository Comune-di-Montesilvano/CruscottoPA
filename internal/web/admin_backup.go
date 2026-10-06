package web

import (
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"
)

const restoreConfirm = "RIPRISTINA"

type backupSection struct {
	Backups []backup.Info
	Status  backup.Status
	Errors  formErrors
	Notice  string
}

func (s *Server) renderBackups(w http.ResponseWriter, status int, errs formErrors, notice string) {
	list, err := s.backup.List()
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "backup_section", backupSection{Backups: list, Status: s.backup.Status(), Errors: errs, Notice: notice})
}

func (s *Server) handleBackupPage(w http.ResponseWriter, r *http.Request) {
	list, err := s.backup.List()
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_backup.html", "backup", backupSection{Backups: list, Status: s.backup.Status()})
}

func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	info, err := s.backup.Create(backup.KindManual)
	if err != nil {
		if !errors.Is(err, backup.ErrBusy) {
			slog.Error("backup manuale non riuscito", "err", err)
		}
		s.renderBackups(w, http.StatusUnprocessableEntity, formErrors{"general": "Backup non riuscito: " + err.Error()}, "")
		return
	}
	slog.Info("backup manuale creato", "user", auth.SafeLog(s.currentAdmin(r)), "file", info.Name)
	s.renderBackups(w, http.StatusOK, nil, "Backup creato: "+info.Name)
}

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := s.backup.Path(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, p)
}

func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	err := s.backup.Delete(r.PathValue("name"))
	switch {
	case errors.Is(err, backup.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, backup.ErrProtected):
		s.renderBackups(w, http.StatusUnprocessableEntity, formErrors{"general": err.Error()}, "")
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderBackups(w, http.StatusOK, nil, "Backup eliminato.")
	}
}

func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("conferma") != restoreConfirm {
		s.renderBackups(w, http.StatusUnprocessableEntity, formErrors{"general": "Per confermare il ripristino digita RIPRISTINA."}, "")
		return
	}
	p, err := s.backup.Path(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.startRestore(w, r, p, false)
}

// startRestore valida l'archivio, crea il pre-ripristino, risponde con la pagina
// di attesa e solo dopo esegue lo swap (che termina il processo).
func (s *Server) startRestore(w http.ResponseWriter, r *http.Request, archive string, removeArchive bool) {
	swap, err := s.backup.PrepareRestore(archive, removeArchive)
	if err != nil {
		var ve *backup.ValidationError
		msg := "Ripristino non riuscito: " + err.Error()
		switch {
		case errors.As(err, &ve):
			msg = "Archivio rifiutato: " + ve.Msg
		case errors.Is(err, backup.ErrBusy):
			msg = err.Error()
		default:
			slog.Error("ripristino non riuscito", "err", err)
		}
		s.renderBackups(w, http.StatusUnprocessableEntity, formErrors{"general": msg}, "")
		return
	}
	slog.Warn("ripristino avviato", "user", auth.SafeLog(s.currentAdmin(r)), "archivio", filepath.Base(archive))
	s.render(w, http.StatusOK, "backup_restarting", nil)
	go func() {
		time.Sleep(s.restoreDelay)
		swap()
	}()
}

// backupWarning è il messaggio per la panoramica admin ("" se tutto ok).
func backupWarning(st backup.Status, now time.Time) string {
	switch {
	case st.LastAutoError != "":
		return "L'ultimo backup automatico non è riuscito: " + st.LastAutoError
	case st.LastSuccess.IsZero():
		return "Non è ancora stato fatto nessun backup."
	case now.Sub(st.LastSuccess) > 48*time.Hour:
		return "L'ultimo backup riuscito ha più di 48 ore."
	}
	return ""
}
