package errtrack

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func newLogger(t *testing.T) (*slog.Logger, *bytes.Buffer, func() []*sentryEvent) {
	tr, events := NewForTest(t)
	var buf bytes.Buffer
	return slog.New(tr.Handler(slog.NewTextHandler(&buf, nil))), &buf, events
}

func TestHandlerError(t *testing.T) {
	log, buf, events := newLogger(t)
	log.Error("ticket: registro", "err", errors.New("disco pieno"))
	ev := one(t, events())
	if !strings.HasPrefix(ev.Fingerprint[0], "ticket: registro|") || !strings.Contains(ev.Fingerprint[0], "TestHandlerError") {
		t.Fatalf("fingerprint = %v", ev.Fingerprint)
	}
	if ev.Contexts["dettagli"]["err"] != "disco pieno" {
		t.Fatalf("dettagli = %v", ev.Contexts["dettagli"])
	}
	if !strings.Contains(buf.String(), "ticket: registro") {
		t.Fatal("log testuale perso")
	}
}

func TestHandlerWarn(t *testing.T) {
	log, buf, events := newLogger(t)
	log.Warn("AD giù")
	log.Info("avvio")
	if len(events()) != 0 {
		t.Fatal("warn/info inviati")
	}
	if !strings.Contains(buf.String(), "AD giù") {
		t.Fatal("log testuale perso")
	}
}

func TestHandlerSkip(t *testing.T) {
	log, _, events := newLogger(t)
	log.Error("panic", SkipKey, true)
	if len(events()) != 0 {
		t.Fatal("record con SkipKey inviato")
	}
}

func TestHandlerWithAttrs(t *testing.T) {
	log, _, events := newLogger(t)
	log.With("modulo", "ticket").WithGroup("otrs").Error("lettura", "stato", 502, "mail", "a@example.it")
	d := one(t, events()).Contexts["dettagli"]
	if d["modulo"] != "ticket" || d["otrs.stato"] != "502" {
		t.Fatalf("dettagli = %v", d)
	}
	if _, ok := d["otrs.mail"]; ok {
		t.Fatal("attributo personale nel gruppo inviato")
	}
}

func TestHandlerPCZero(t *testing.T) {
	tr, events := NewForTest(t)
	h := tr.Handler(slog.NewTextHandler(&bytes.Buffer{}, nil))
	_ = h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelError, "senza pc", 0))
	if fp := one(t, events()).Fingerprint[0]; fp != "senza pc" {
		t.Fatalf("fingerprint = %q", fp)
	}
}

func TestHandlerNil(t *testing.T) {
	var tr *Tracker
	next := slog.NewTextHandler(&bytes.Buffer{}, nil)
	if tr.Handler(next) != slog.Handler(next) {
		t.Fatal("tracker nil deve restituire next")
	}
}
