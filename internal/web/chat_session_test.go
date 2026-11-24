package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// recordingAuth registra lo username passato a Verify.
type recordingAuth struct {
	fakeAuth
	got *string
}

func (a recordingAuth) Verify(u, _ string) (bool, error) {
	*a.got = u
	return a.ok, a.err
}

func chatJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("stato %d (atteso 200)\n%s", rec.Code, rec.Body)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("JSON: %v\n%s", err, rec.Body)
	}
	return m
}

func setCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestChatLoginOK(t *testing.T) {
	var got string
	s, _ := newChatServer(t, func(o *Options) { o.Auth = recordingAuth{fakeAuth{ok: true}, &got} })
	u := userCookie(t, s, "mrossi", "Mario Rossi")
	rec := do(t, s, "POST", "/chat/accesso", url.Values{"password": {"x"}}, u, nil)
	if m := chatJSON(t, rec); m["ok"] != true {
		t.Fatalf("esito: %v", m)
	}
	c := setCookie(rec, identity.ChatCookieName)
	if c == nil || !c.HttpOnly || c.Path != "/" || c.MaxAge != int(identity.ChatTTL.Seconds()) {
		t.Fatalf("cookie: %+v", c)
	}
	if sess, ok := s.chatCookies.Decode(c.Value); !ok || sess.Username != "mrossi" {
		t.Fatalf("sessione: %+v %v", sess, ok)
	}
	if got != "mrossi" {
		t.Fatalf("Verify con %q", got)
	}
}

func TestChatLoginIgnoresUsernameField(t *testing.T) {
	var got string
	s, _ := newChatServer(t, func(o *Options) { o.Auth = recordingAuth{fakeAuth{ok: true}, &got} })
	rec := do(t, s, "POST", "/chat/accesso", url.Values{"username": {"abianchi"}, "password": {"x"}}, userCookie(t, s, "mrossi", "Mario Rossi"), nil)
	chatJSON(t, rec)
	c := setCookie(rec, identity.ChatCookieName)
	if got != "mrossi" || c == nil {
		t.Fatalf("Verify con %q, cookie %v", got, c)
	}
	if sess, _ := s.chatCookies.Decode(c.Value); sess.Username != "mrossi" {
		t.Fatalf("cookie per %q", sess.Username)
	}
}

func TestChatLoginWrongPassword(t *testing.T) {
	s, _ := newChatServer(t, func(o *Options) { o.Auth = fakeAuth{ok: false} })
	u := userCookie(t, s, "mrossi", "Mario Rossi")
	for i := 0; i < 5; i++ {
		rec := do(t, s, "POST", "/chat/accesso", url.Values{"password": {"x"}}, u, nil)
		if m := chatJSON(t, rec); m["errore"] != "credenziali" || setCookie(rec, identity.ChatCookieName) != nil {
			t.Fatalf("tentativo %d: %v", i, m)
		}
	}
	if m := chatJSON(t, do(t, s, "POST", "/chat/accesso", url.Values{"password": {"x"}}, u, nil)); m["errore"] != "bloccato" {
		t.Fatalf("dopo 5 errori: %v", m)
	}
}

func TestChatLoginErrors(t *testing.T) {
	s, _ := newChatServer(t, nil)
	if m := chatJSON(t, do(t, s, "POST", "/chat/accesso", url.Values{"password": {"x"}}, nil, nil)); m["errore"] != "anonimo" {
		t.Fatalf("senza cookie utente: %v", m)
	}
	if m := chatJSON(t, do(t, s, "POST", "/chat/accesso", url.Values{"password": {""}}, userCookie(t, s, "mrossi", "Mario Rossi"), nil)); m["errore"] != "credenziali" {
		t.Fatalf("password vuota: %v", m)
	}
	s2, _ := newChatServer(t, func(o *Options) { o.Auth = fakeAuth{err: errAD} })
	if m := chatJSON(t, do(t, s2, "POST", "/chat/accesso", url.Values{"password": {"x"}}, userCookie(t, s2, "mrossi", "Mario Rossi"), nil)); m["errore"] != "ad" {
		t.Fatalf("AD giù: %v", m)
	}
}

func TestChatLogout(t *testing.T) {
	s, _ := newChatServer(t, nil)
	rec := do(t, s, "POST", "/chat/esci", nil, nil, withCookies(nil, userCookie(t, s, "mrossi", "Mario Rossi"), chatCookie(t, s, "mrossi")))
	chatJSON(t, rec)
	if c := setCookie(rec, identity.ChatCookieName); c == nil || c.MaxAge >= 0 {
		t.Fatalf("cookie non cancellato: %+v", c)
	}
}

func chatViewerOf(t *testing.T, s *Server, cs ...*http.Cookie) (chatViewer, bool, *httptest.ResponseRecorder) {
	t.Helper()
	r := httptest.NewRequest("GET", "/", nil)
	for _, c := range cs {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	v, ok := s.chatViewer(w, r)
	return v, ok, w
}

// Review Focus 1: cookie della chat di un altro utente nello stesso browser.
func TestChatCookieOtherNTLMUser(t *testing.T) {
	s, _ := newChatServer(t, nil)
	v, ok, _ := chatViewerOf(t, s, userCookie(t, s, "abianchi", "Anna Bianchi"), chatCookie(t, s, "mrossi"))
	if !ok || v.Username != "abianchi" || v.Verified {
		t.Fatalf("viewer: %+v %v", v, ok)
	}
	v, ok, _ = chatViewerOf(t, s, userCookie(t, s, "mrossi", "Mario Rossi"), chatCookie(t, s, "MRossi"))
	if !ok || !v.Verified {
		t.Fatalf("stesso utente: %+v %v", v, ok)
	}
}

func TestChatCookieRenewed(t *testing.T) {
	s, _ := newChatServer(t, nil)
	old := identity.NewChatCodec(strings.Repeat("s", 32))
	old.SetClock(func() time.Time { return fixedNow.Add(-48 * time.Hour) })
	v, _ := old.Encode(identity.ChatSession{Username: "mrossi"})
	cv, ok, w := chatViewerOf(t, s, userCookie(t, s, "mrossi", "Mario Rossi"), &http.Cookie{Name: identity.ChatCookieName, Value: v})
	if !ok || !cv.Verified || setCookie(w, identity.ChatCookieName) == nil {
		t.Fatalf("rinnovo mancante: %+v %v", cv, ok)
	}
	old.SetClock(func() time.Time { return fixedNow.Add(-time.Hour) })
	v, _ = old.Encode(identity.ChatSession{Username: "mrossi"})
	if _, _, w := chatViewerOf(t, s, userCookie(t, s, "mrossi", "Mario Rossi"), &http.Cookie{Name: identity.ChatCookieName, Value: v}); setCookie(w, identity.ChatCookieName) != nil {
		t.Fatal("cookie riscritto dopo un'ora")
	}
}

func TestChatViewerOutsideAudience(t *testing.T) {
	s, _ := newChatServer(t, nil)
	if _, ok, _ := chatViewerOf(t, s, userCookie(t, s, "sconosciuto", "X")); ok {
		t.Fatal("utente sconosciuto ad AD ammesso")
	}
	if _, ok, _ := chatViewerOf(t, s); ok {
		t.Fatal("anonimo ammesso")
	}
}
