// Package guidesrc scarica le guide Markdown da GitHub (repository pubblici).
package guidesrc

import (
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var ErrBadURL = errors.New("Usa il link a un file .md su GitHub, es. https://github.com/org/repo/blob/main/docs/guida.md")

// Source: dove scaricare il file e come riscrivere link e immagini relativi.
type Source struct {
	Raw       string // https://raw.githubusercontent.com/{owner}/{repo}/{ref}/{path}
	LinkBase  string // https://github.com/{owner}/{repo}/blob/{ref}/{dir}/
	ImageBase string // https://raw.githubusercontent.com/{owner}/{repo}/{ref}/{dir}/
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ParseGitHubURL accetta solo https://github.com/{owner}/{repo}/blob/{ref}/{path}.md.
// Un ref con "/" (es. feature/x) non si distingue dal percorso: il primo
// segmento dopo blob è il ref.
func ParseGitHubURL(s string) (Source, error) {
	if s == "" || len(s) > 1024 || strings.ContainsAny(s, " \t\r\n\\") {
		return Source{}, ErrBadURL
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" {
		return Source{}, ErrBadURL
	}
	parts := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	if len(parts) < 5 || parts[2] != "blob" || !nameRe.MatchString(parts[0]) || !nameRe.MatchString(parts[1]) {
		return Source{}, ErrBadURL
	}
	for _, p := range parts[3:] {
		if p == "" || p == "." || p == ".." || strings.Contains(p, "%2") {
			return Source{}, ErrBadURL // %2F, %2E: niente percorsi nascosti nella codifica
		}
	}
	file := strings.Join(parts[4:], "/")
	if !strings.EqualFold(path.Ext(file), ".md") {
		return Source{}, ErrBadURL
	}
	owner, repo, ref := parts[0], parts[1], parts[3]
	dir := path.Dir(file)
	if dir == "." {
		dir = ""
	} else {
		dir += "/"
	}
	raw := "https://raw.githubusercontent.com/" + owner + "/" + repo + "/" + ref + "/"
	return Source{
		Raw:       raw + file,
		LinkBase:  "https://github.com/" + owner + "/" + repo + "/blob/" + ref + "/" + dir,
		ImageBase: raw + dir,
	}, nil
}
