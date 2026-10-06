package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// formErrors: campo → messaggio. "general" per errori non legati a un campo.
type formErrors map[string]string

func (e formErrors) add(field, msg string) {
	if _, ok := e[field]; !ok {
		e[field] = msg
	}
}

func checkText(e formErrors, field, value string, max int, required bool) {
	switch {
	case required && strings.TrimSpace(value) == "":
		e.add(field, "Campo obbligatorio.")
	case utf8.RuneCountInString(value) > max:
		e.add(field, fmt.Sprintf("Massimo %d caratteri.", max))
	}
}

func checkURL(e formErrors, field, value string, required bool) {
	if value == "" {
		if required {
			e.add(field, "Campo obbligatorio.")
		}
		return
	}
	if !validURL(value) {
		e.add(field, "Inserisci l'indirizzo completo, es. https://…")
	}
}

// validURL accetta solo URL assoluti http/https con host, senza spazi.
func validURL(s string) bool {
	if s == "" || len(s) > 2000 || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func checkColor(e formErrors, field, value string) {
	if !colorRe.MatchString(value) {
		e.add(field, "Colore non valido (formato #rrggbb).")
	}
}

var errBadID = errors.New("id non valido")

// pathID legge {id} dalla route; 0,nil se la route non lo prevede.
func pathID(r *http.Request) (int64, error) {
	v := r.PathValue("id")
	if v == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		return 0, errBadID
	}
	return id, nil
}

func moveDir(r *http.Request) int {
	if r.FormValue("dir") == "up" {
		return -1
	}
	return 1
}
