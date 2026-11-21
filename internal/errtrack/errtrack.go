// Package errtrack manda gli errori del Cruscotto a GlitchTip (API Sentry)
// senza dati identificanti: utente solo come hash, testo e URL ripuliti,
// anti-pioggia comune a tutti i percorsi.
package errtrack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

const (
	maxMessage = 500
	maxStack   = 8 << 10
	maxUA      = 300
	serverName = "cruscottopa"
	// detailsKey: contesto con gli attributi dell'errore (sentry-go non ha più Extra).
	detailsKey = "dettagli"
)

type Options struct {
	DSN, Environment, Release string
	// Secret: SESSION_SECRET, da cui deriva la chiave dell'hash degli utenti.
	Secret    string
	Transport sentry.Transport // nil = HTTP (proxy da HTTPS_PROXY)
	Now       func() time.Time
}

// Tracker: nil = integrazione spenta; tutti i metodi sono sicuri su nil.
type Tracker struct {
	client   *sentry.Client
	userKey  []byte
	throttle *throttle
}

// New: DSN vuota → (nil, nil); DSN non valida → errore.
func New(o Options) (*Tracker, error) {
	dsn := strings.TrimSpace(o.DSN)
	if dsn == "" {
		return nil, nil
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:                    dsn,
		Environment:            o.Environment,
		Release:                o.Release,
		ServerName:             serverName,
		SendDefaultPII:         false,
		Transport:              o.Transport,
		DisableTelemetryBuffer: true,
		BeforeSend:             scrubEvent,
		// Nessuna integrazione automatica: niente moduli, ambiente o richieste.
		Integrations: func([]sentry.Integration) []sentry.Integration { return nil },
	})
	if err != nil {
		return nil, err
	}
	key := sha256.Sum256([]byte("errori-utente:" + o.Secret))
	return &Tracker{client: client, userKey: key[:], throttle: newThrottle(o.Now)}, nil
}

func (t *Tracker) Enabled() bool { return t != nil }

// UserID: hash non reversibile senza SESSION_SECRET; "" per l'anonimo.
func (t *Tracker) UserID(username string) string {
	if t == nil || username == "" {
		return ""
	}
	m := hmac.New(sha256.New, t.userKey)
	m.Write([]byte(strings.ToLower(username)))
	return hex.EncodeToString(m.Sum(nil))[:16]
}

// Request: dati della richiesta HTTP che possono uscire (il percorso viene normalizzato).
type Request struct{ Method, Path string }

// CaptureMessage: errore del server (slog). attrs con chiavi personali vengono scartati.
func (t *Tracker) CaptureMessage(msg, fingerprint string, attrs map[string]string, req *Request, userID string) {
	if t == nil {
		return
	}
	ev := t.event(sentry.LevelError, "server", fingerprint, req, userID)
	ev.Message = truncate(msg, maxMessage)
	for k, v := range attrs {
		if !PersonalKey(k) {
			ev.Contexts[detailsKey][k] = truncate(v, maxStack)
		}
	}
	t.send(ev, fingerprint, userID)
}

// CapturePanic: panic in un handler HTTP. Va chiamata dentro la recover,
// così lo stack parte dal punto del panic.
func (t *Tracker) CapturePanic(v any, req Request, userID string) {
	if t == nil {
		return
	}
	val := truncate(fmt.Sprint(v), maxMessage)
	typ := fmt.Sprintf("%T", v)
	fp := "panic|" + typ + "|" + val
	if st := sentry.NewStacktrace(); st != nil && len(st.Frames) > 0 {
		f := st.Frames[len(st.Frames)-1]
		fp = "panic|" + f.Function + ":" + strconv.Itoa(f.Lineno)
		ev := t.event(sentry.LevelFatal, "server", fp, &req, userID)
		ev.Exception = []sentry.Exception{{Type: typ, Value: val, Stacktrace: st}}
		t.send(ev, fp, userID)
		return
	}
	ev := t.event(sentry.LevelFatal, "server", fp, &req, userID)
	ev.Exception = []sentry.Exception{{Type: typ, Value: val}}
	t.send(ev, fp, userID)
}

// BrowserError: quanto manda errori.js (già validato da chi chiama).
type BrowserError struct {
	Message, Stack, File string
	Line, Col            int
	Page, UserAgent      string
}

func (t *Tracker) CaptureBrowser(e BrowserError, userID string) {
	if t == nil {
		return
	}
	fp := e.Message + "|" + e.File + "|" + strconv.Itoa(e.Line)
	ev := t.event(sentry.LevelError, "browser", fp, nil, userID)
	ev.Platform = "javascript"
	ev.Message = truncate(e.Message, maxMessage)
	ev.Tags["pagina"] = NormalizePath(e.Page)
	ev.Contexts[detailsKey]["file"] = e.File
	ev.Contexts[detailsKey]["riga"] = e.Line
	ev.Contexts[detailsKey]["colonna"] = e.Col
	if e.Stack != "" {
		ev.Contexts[detailsKey]["stack"] = truncate(e.Stack, maxStack)
	}
	if e.UserAgent != "" {
		ev.Contexts[detailsKey]["browser"] = truncate(e.UserAgent, maxUA)
	}
	t.send(ev, fp, userID)
}

func (t *Tracker) Flush(d time.Duration) {
	if t != nil {
		t.client.Flush(d)
	}
}

func (t *Tracker) event(level sentry.Level, origin, fp string, req *Request, userID string) *sentry.Event {
	ev := sentry.NewEvent()
	ev.Level = level
	ev.Platform = "go"
	ev.Fingerprint = []string{fp}
	ev.Tags = map[string]string{"origine": origin}
	ev.Contexts = map[string]sentry.Context{detailsKey: {}}
	if req != nil {
		ev.Request = &sentry.Request{Method: req.Method, URL: NormalizePath(req.Path)}
	}
	if userID != "" {
		ev.User = sentry.User{ID: userID}
	}
	return ev
}

func (t *Tracker) send(ev *sentry.Event, fp, userID string) {
	if !t.throttle.allow(fp + "|" + userID) {
		return
	}
	t.client.CaptureEvent(ev, nil, nil)
}

// scrubEvent è l'ultima barriera prima dell'invio: vale per ogni evento.
func scrubEvent(ev *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	ev.Message = Scrub(ev.Message)
	ev.ServerName = serverName
	for i := range ev.Exception {
		ev.Exception[i].Value = Scrub(ev.Exception[i].Value)
	}
	details := ev.Contexts[detailsKey]
	for k, v := range details {
		if PersonalKey(k) {
			delete(details, k)
			continue
		}
		if s, ok := v.(string); ok {
			details[k] = Scrub(s)
		}
	}
	for k, v := range ev.Tags {
		ev.Tags[k] = Scrub(v)
	}
	ev.User = sentry.User{ID: ev.User.ID}
	if ev.Request != nil {
		ev.Request = &sentry.Request{Method: ev.Request.Method, URL: NormalizePath(ev.Request.URL)}
	}
	ev.Modules = nil
	ev.Contexts = nil
	if len(details) > 0 {
		ev.Contexts = map[string]sentry.Context{detailsKey: details}
	}
	ev.Breadcrumbs = nil
	return ev
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
