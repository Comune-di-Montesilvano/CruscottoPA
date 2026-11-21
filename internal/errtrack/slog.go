package errtrack

import (
	"context"
	"log/slog"
	"runtime"
)

// SkipKey: un record con slog.Bool(SkipKey, true) resta solo nel log
// (es. il panic, già inviato come eccezione dal middleware).
const SkipKey = "errtrack.skip"

// Handler avvolge next: tutto va a next come prima; i record di livello
// Error diventano anche un evento. Tracker nil → next.
func (t *Tracker) Handler(next slog.Handler) slog.Handler {
	if t == nil {
		return next
	}
	return &teeHandler{next: next, t: t}
}

type teeHandler struct {
	next   slog.Handler
	t      *Tracker
	attrs  map[string]string // già con il prefisso dei gruppi
	prefix string
}

func (h *teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= slog.LevelError || h.next.Enabled(ctx, l)
}

func (h *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var err error
	if h.next.Enabled(ctx, r.Level) {
		err = h.next.Handle(ctx, r)
	}
	if r.Level < slog.LevelError {
		return err
	}
	attrs := make(map[string]string, len(h.attrs)+r.NumAttrs())
	for k, v := range h.attrs {
		attrs[k] = v
	}
	skip := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == SkipKey {
			skip = true
			return false
		}
		flatten(attrs, h.prefix, a)
		return true
	})
	if skip {
		return err
	}
	fp := r.Message
	if r.PC != 0 {
		if f, _ := runtime.CallersFrames([]uintptr{r.PC}).Next(); f.Function != "" {
			fp += "|" + f.Function
		}
	}
	h.t.CaptureMessage(r.Message, fp, attrs, nil, "")
	return err
}

func (h *teeHandler) WithAttrs(as []slog.Attr) slog.Handler {
	attrs := make(map[string]string, len(h.attrs)+len(as))
	for k, v := range h.attrs {
		attrs[k] = v
	}
	for _, a := range as {
		flatten(attrs, h.prefix, a)
	}
	return &teeHandler{next: h.next.WithAttrs(as), t: h.t, attrs: attrs, prefix: h.prefix}
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &teeHandler{next: h.next.WithGroup(name), t: h.t, attrs: h.attrs, prefix: h.prefix + name + "."}
}

func flatten(dst map[string]string, prefix string, a slog.Attr) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		p := prefix
		if a.Key != "" {
			p += a.Key + "."
		}
		for _, g := range v.Group() {
			flatten(dst, p, g)
		}
		return
	}
	if a.Key == "" {
		return
	}
	dst[prefix+a.Key] = v.String()
}
