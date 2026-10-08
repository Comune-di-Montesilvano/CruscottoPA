package web

import (
	"errors"
	"html"
	"html/template"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// Pagina "Gruppi": attributi AD utilizzabili, gruppi della plancia e regole.
// I gruppi decidono la visibilità dei contenuti: filtro di presentazione, non
// protezione (l'identità in plancia è dichiarata).

type audienceSection struct {
	Attributes []database.AudienceAttribute
	Groups     []database.AudienceGroup
	Edit       *groupEdit
	Errors     formErrors
}

// groupEdit: le regole divise come le legge l'admin. Un utente è nel gruppo
// se soddisfa una regola di Include, tutte quelle di Require e nessuna di
// Exclude.
type groupEdit struct {
	Group      database.AudienceGroup
	Include    []ruleView // "Chi entra": basta una
	Require    []ruleView // "Requisiti": servono tutti
	Exclude    []ruleView // "Esclusi"
	Summary    template.HTML
	Attributes []database.AudienceAttribute
	Preview    *previewView
	Errors     formErrors
	Form       database.AudienceRule // valori da riproporre dopo un errore
	FormBlock  string                // blocco del form con l'errore: include, require, exclude
}

type ruleView struct {
	database.AudienceRule
	Text string
}

type previewView struct {
	Count  int
	People []identity.Person
	Attrs  []database.AudienceAttribute // colonne: attributi usati dalle regole
	Err    string
}

// draftView: anteprima di una regola non ancora salvata.
type draftView struct {
	Err    string
	Alone  string // "Questa regola: N utenti" (vuoto per "Escludi")
	Change string // "Il gruppo passerebbe da X a Y utenti"
}

// suggestion: Field è il campo del form da riempire ("" = value).
type suggestion struct{ Value, Label, Text, Field string }

type suggestionsView struct {
	Err   string
	Items []suggestion
}

var ruleUserRe = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,128}$`)

const adUnavailable = "AD non disponibile."

func attrLabel(name string, attrs []database.AudienceAttribute) string {
	for _, a := range attrs {
		if strings.EqualFold(a.Name, name) {
			return a.Label
		}
	}
	return name
}

// ruleBlock: in quale blocco dell'editor sta un tipo di regola.
func ruleBlock(kind string) string {
	switch {
	case kind == "":
		return ""
	case kind == audience.KindExclude:
		return "exclude"
	case audience.IsRequirement(kind):
		return "require"
	}
	return "include"
}

func ruleText(r database.AudienceRule, attrs []database.AudienceAttribute) string {
	switch r.Kind {
	case audience.KindAttr:
		return attrLabel(r.Attr, attrs) + " = " + r.Value
	case audience.KindPresent:
		return attrLabel(r.Attr, attrs) + " presente"
	case audience.KindAbsent:
		return attrLabel(r.Attr, attrs) + " assente"
	case audience.KindADGroup:
		if r.Label != "" {
			return "Gruppo AD " + r.Label
		}
		return "Gruppo AD " + r.Value
	case audience.KindUser:
		return "Utente " + r.Value
	default:
		return r.Value
	}
}

// ruleSummary: le regole del gruppo in una frase, es. "Entra chi ha Ufficio =
// TRIBUTI oppure Gruppo AD X, purché abbia Email; escluso mrossi."
func ruleSummary(e *groupEdit) template.HTML {
	if len(e.Include) == 0 {
		return "Nessuna regola in «Chi entra»: il gruppo non ha membri."
	}
	strong := func(s string) string { return "<strong>" + html.EscapeString(s) + "</strong>" }
	var b strings.Builder
	b.WriteString("Entra chi ha ")
	for i, r := range e.Include {
		if i > 0 {
			b.WriteString(" oppure ")
		}
		b.WriteString(strong(r.Text))
	}
	var has, hasNot []string
	for _, r := range e.Require {
		if r.Kind == audience.KindPresent {
			has = append(has, strong(attrLabel(r.Attr, e.Attributes)))
		} else {
			hasNot = append(hasNot, strong(attrLabel(r.Attr, e.Attributes)))
		}
	}
	if len(has)+len(hasNot) > 0 {
		b.WriteString(", purché ")
		if len(has) > 0 {
			b.WriteString("abbia " + strings.Join(has, " e "))
		}
		if len(has) > 0 && len(hasNot) > 0 {
			b.WriteString(" e ")
		}
		if len(hasNot) > 0 {
			b.WriteString("non abbia " + strings.Join(hasNot, " né "))
		}
	}
	if len(e.Exclude) > 0 {
		names := make([]string, len(e.Exclude))
		for i, r := range e.Exclude {
			names[i] = strong(r.Value)
		}
		if len(names) == 1 {
			b.WriteString("; escluso ")
		} else {
			b.WriteString("; esclusi ")
		}
		b.WriteString(strings.Join(names, ", "))
	}
	b.WriteString(".")
	return template.HTML(b.String())
}

// audienceData carica la pagina; con editID != 0 anche il gruppo in modifica.
func (s *Server) audienceData(editID int64, errs, editErrs formErrors, ruleForm database.AudienceRule) (audienceSection, error) {
	sec := audienceSection{Errors: errs}
	var err error
	if sec.Attributes, err = s.db.ListAudienceAttributes(); err != nil {
		return sec, err
	}
	// Prima quelli sotto il saluto, nell'ordine della testata (si vedono le
	// frecce funzionare); poi gli altri, per etichetta.
	slices.SortStableFunc(sec.Attributes, func(a, b database.AudienceAttribute) int {
		switch {
		case a.Hero > 0 && b.Hero > 0:
			return a.Hero - b.Hero
		case a.Hero > 0:
			return -1
		case b.Hero > 0:
			return 1
		}
		return 0
	})
	if sec.Groups, err = s.db.ListAudienceGroups(); err != nil {
		return sec, err
	}
	if editID == 0 {
		return sec, nil
	}
	g, err := s.db.GetAudienceGroup(editID)
	if err != nil {
		return sec, err
	}
	rules, err := s.db.ListAudienceRules(editID)
	if err != nil {
		return sec, err
	}
	e := &groupEdit{Group: g, Attributes: sec.Attributes, Errors: editErrs, Form: ruleForm, FormBlock: ruleBlock(ruleForm.Kind)}
	for _, r := range rules {
		v := ruleView{AudienceRule: r, Text: ruleText(r, sec.Attributes)}
		switch ruleBlock(r.Kind) {
		case "exclude":
			e.Exclude = append(e.Exclude, v)
		case "require":
			e.Require = append(e.Require, v)
		default:
			e.Include = append(e.Include, v)
		}
	}
	e.Summary = ruleSummary(e)
	sec.Edit = e
	return sec, nil
}

func (s *Server) renderAudience(w http.ResponseWriter, status int, editID int64, errs, editErrs formErrors) {
	s.renderAudienceForm(w, status, editID, errs, editErrs, database.AudienceRule{})
}

// renderAudienceForm: come renderAudience, ma ripropone i valori della regola
// appena inviata (dopo un errore di validazione).
func (s *Server) renderAudienceForm(w http.ResponseWriter, status int, editID int64, errs, editErrs formErrors, ruleForm database.AudienceRule) {
	sec, err := s.audienceData(editID, errs, editErrs, ruleForm)
	if errors.Is(err, database.ErrNotFound) {
		http.Error(w, "Gruppo non trovato", http.StatusNotFound)
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "audience_section", sec)
}

func statusFor(errs formErrors) int {
	if len(errs) > 0 {
		return http.StatusUnprocessableEntity
	}
	return http.StatusOK
}

func (s *Server) handleAudiencePage(w http.ResponseWriter, r *http.Request) {
	sec, err := s.audienceData(0, nil, nil, database.AudienceRule{})
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_gruppi.html", "gruppi", sec)
}

// ── Attributi ──────────────────────────────────────────────────────────

func (s *Server) handleAttributeAdd(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	label := strings.TrimSpace(r.FormValue("label"))
	errs := formErrors{}
	if !identity.ValidAttrName(name) {
		errs.add("attr", "Nome di attributo LDAP non valido.")
	}
	checkText(errs, "attr", label, 60, true)
	if len(errs) == 0 {
		_, err := s.db.CreateAudienceAttribute(name, label)
		switch {
		case errors.Is(err, database.ErrDuplicate):
			errs.add("attr", "Attributo già presente.")
		case err != nil:
			s.serverError(w, err)
			return
		default:
			s.profiles.reset()
		}
	}
	s.renderAudience(w, statusFor(errs), 0, errs, nil)
}

func (s *Server) handleAttributeDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	errs := formErrors{}
	switch err := s.db.DeleteAudienceAttribute(id); {
	case errors.Is(err, database.ErrInUse):
		errs.add("general", "Attributo usato da qualche regola: toglile prima.")
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		s.serverError(w, err)
		return
	default:
		s.profiles.reset()
	}
	s.renderAudience(w, statusFor(errs), 0, errs, nil)
}

// handleAttributeHero: mostra (con un formato) o toglie l'attributo dalla
// riga sotto il saluto in plancia.
func (s *Server) handleAttributeHero(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	kind := r.FormValue("hero_kind")
	errs := formErrors{}
	if kind != "" && !database.ValidHeroKind(kind) {
		errs.add("attr", "Formato non valido.")
	} else if err := s.db.SetAttributeHero(id, kind); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderAudience(w, statusFor(errs), 0, errs, nil)
}

func (s *Server) handleAttributeHeroMove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.MoveAttributeHero(id, moveDir(r)); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderAudience(w, http.StatusOK, 0, nil, nil)
}

// ── Gruppi ─────────────────────────────────────────────────────────────

func (s *Server) handleAudienceGroupCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	errs := formErrors{}
	checkText(errs, "name", name, 60, true)
	if len(errs) == 0 {
		if _, err := s.db.CreateAudienceGroup(name); errors.Is(err, database.ErrDuplicate) {
			errs.add("name", "Esiste già un gruppo con questo nome.")
		} else if err != nil {
			s.serverError(w, err)
			return
		}
	}
	s.renderAudience(w, statusFor(errs), 0, errs, nil)
}

func (s *Server) handleAudienceGroupRename(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	editErrs := formErrors{}
	checkText(editErrs, "rule", name, 60, true)
	if len(editErrs) == 0 {
		switch err := s.db.RenameAudienceGroup(id, name); {
		case errors.Is(err, database.ErrDuplicate):
			editErrs.add("rule", "Esiste già un gruppo con questo nome.")
		case errors.Is(err, database.ErrNotFound):
			http.NotFound(w, r)
			return
		case err != nil:
			s.serverError(w, err)
			return
		}
	}
	s.renderAudience(w, statusFor(editErrs), id, nil, editErrs)
}

func (s *Server) handleAudienceGroupEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.renderAudience(w, http.StatusOK, id, nil, nil)
}

func (s *Server) handleAudienceGroupMove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.MoveAudienceGroup(id, moveDir(r)); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderAudience(w, http.StatusOK, 0, nil, nil)
}

// handleAudienceGroupDelete: bloccato se il gruppo è usato, altrimenti un
// contenuto "Riservato a" diventerebbe pubblico.
func (s *Server) handleAudienceGroupDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	errs := formErrors{}
	switch err := s.db.DeleteAudienceGroup(id); {
	case errors.Is(err, database.ErrInUse):
		uses, uerr := s.db.GroupUses(id)
		if uerr != nil {
			s.serverError(w, uerr)
			return
		}
		errs.add("general", "Gruppo usato da: "+strings.Join(uses, ", ")+". Cambia prima la visibilità di questi contenuti.")
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		s.serverError(w, err)
		return
	}
	s.renderAudience(w, statusFor(errs), 0, errs, nil)
}

// ── Regole ─────────────────────────────────────────────────────────────

func (s *Server) handleRuleAdd(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.db.GetAudienceGroup(id); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	rule := database.AudienceRule{
		GroupID: id,
		Kind:    r.FormValue("kind"),
		Attr:    strings.TrimSpace(r.FormValue("attr")),
		Value:   strings.TrimSpace(r.FormValue("value")),
		Label:   strings.TrimSpace(r.FormValue("label")),
	}
	errs := formErrors{}
	switch rule.Kind {
	case audience.KindAttr, audience.KindPresent, audience.KindAbsent:
		attrs, err := s.db.ListAudienceAttributes()
		if err != nil {
			s.serverError(w, err)
			return
		}
		ok := false
		for _, a := range attrs {
			if strings.EqualFold(a.Name, rule.Attr) {
				ok, rule.Attr = true, a.Name
			}
		}
		if !ok {
			errs.add("rule", "Scegli un attributo configurato.")
		}
	case audience.KindUser, audience.KindExclude:
		if !ruleUserRe.MatchString(rule.Value) {
			errs.add("rule", "Username non valido.")
		}
	case audience.KindADGroup:
	default:
		errs.add("rule", "Tipo di regola non valido.")
	}
	if audience.IsRequirement(rule.Kind) {
		rule.Value = ""
	} else {
		checkText(errs, "rule", rule.Value, 512, true)
	}
	checkText(errs, "rule", rule.Label, 256, false)
	if len(errs) == 0 {
		if _, err := s.db.AddAudienceRule(rule); errors.Is(err, database.ErrDuplicate) {
			errs.add("rule", "Regola già presente.")
		} else if errors.Is(err, database.ErrInvalidDN) {
			errs.add("rule", "DN non valido: sceglilo dai suggerimenti (es. CN=Gruppo,OU=…,DC=…).")
		} else if err != nil {
			s.serverError(w, err)
			return
		}
	}
	if len(errs) > 0 {
		s.renderAudienceForm(w, http.StatusUnprocessableEntity, id, nil, errs, rule)
		return
	}
	s.renderAudience(w, http.StatusOK, id, nil, nil)
}

func (s *Server) handleRuleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rid, err := strconv.ParseInt(r.PathValue("rid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.DeleteAudienceRule(id, rid); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderAudience(w, http.StatusOK, id, nil, nil)
}

// handleMemberCount: numero dei membri per la colonna dell'elenco dei gruppi
// (caricato a parte, così la pagina non aspetta AD).
func (s *Server) handleMemberCount(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.db.GetAudienceGroup(id); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	all, err := s.db.AllAudienceRules()
	if err != nil {
		s.serverError(w, err)
		return
	}
	text := "—"
	if n, _, err := s.members(all[id], nil); err == nil {
		text = strconv.Itoa(n)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(text))
}

func (s *Server) handleAudiencePreview(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.db.GetAudienceGroup(id); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	all, err := s.db.AllAudienceRules()
	if err != nil {
		s.serverError(w, err)
		return
	}
	pv := previewView{}
	if pv.Attrs, err = s.ruleAttributes(all[id]); err != nil {
		s.serverError(w, err)
		return
	}
	names := make([]string, len(pv.Attrs))
	for i, a := range pv.Attrs {
		names[i] = a.Name
	}
	if pv.Count, pv.People, err = s.members(all[id], names); err != nil {
		pv.Err = adUnavailable
	}
	s.render(w, http.StatusOK, "group_preview", pv)
}

// ruleAttributes: attributi configurati usati dalle regole (anche dai requisiti), per le colonne
// della tabella dell'anteprima.
func (s *Server) ruleAttributes(rules []audience.Rule) ([]database.AudienceAttribute, error) {
	attrs, err := s.db.ListAudienceAttributes()
	if err != nil {
		return nil, err
	}
	out := []database.AudienceAttribute{}
	for _, a := range attrs {
		for _, r := range rules {
			if r.Attr != "" && strings.EqualFold(r.Attr, a.Name) {
				out = append(out, a)
				break
			}
		}
	}
	return out, nil
}

func peopleCount(n int) string {
	if n == 1 {
		return "1 utente"
	}
	return strconv.Itoa(n) + " utenti"
}

// handleDraftPreview mostra, mentre si compone una regola, quanti utenti
// porterebbe nel gruppo. Non salva nulla.
func (s *Server) handleDraftPreview(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.db.GetAudienceGroup(id); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	draft := audience.Rule{Kind: r.FormValue("kind"), Attr: strings.TrimSpace(r.FormValue("attr")), Value: strings.TrimSpace(r.FormValue("value"))}
	v := draftView{}
	switch {
	case draft.Value == "" && !audience.IsRequirement(draft.Kind):
		v.Err = "Scrivi un valore per vedere l'anteprima."
	case !audience.ValidKind(draft.Kind):
		v.Err = "Tipo di regola non valido."
	case (draft.Kind == audience.KindUser || draft.Kind == audience.KindExclude) && !ruleUserRe.MatchString(draft.Value):
		v.Err = "Username non valido."
	case draft.Kind == audience.KindADGroup:
		if _, err := audience.NormalizeDN(draft.Value); err != nil {
			v.Err = "Scegli il gruppo dai suggerimenti (serve il DN completo)."
		}
	}
	if v.Err == "" && (draft.Kind == audience.KindAttr || audience.IsRequirement(draft.Kind)) {
		attrs, err := s.db.ListAudienceAttributes()
		if err != nil {
			s.serverError(w, err)
			return
		}
		ok := false
		for _, a := range attrs {
			ok = ok || strings.EqualFold(a.Name, draft.Attr)
		}
		if !ok {
			v.Err = "Scegli un attributo configurato."
		}
	}
	if v.Err == "" {
		v = s.draftCounts(id, draft)
	}
	s.render(w, http.StatusOK, "draft_preview", v)
}

func (s *Server) draftCounts(groupID int64, draft audience.Rule) draftView {
	all, err := s.db.AllAudienceRules()
	if err != nil {
		return draftView{Err: "Regole non disponibili."}
	}
	current := all[groupID]
	v := draftView{}
	if ruleBlock(draft.Kind) == "include" {
		n, _, err := s.members([]audience.Rule{draft}, nil)
		if err != nil {
			return draftView{Err: adUnavailable}
		}
		v.Alone = "Questa regola: " + peopleCount(n)
	}
	before, _, err := s.members(current, nil)
	if err != nil {
		return draftView{Err: adUnavailable}
	}
	after, _, err := s.members(append(append([]audience.Rule{}, current...), draft), nil)
	if err != nil {
		return draftView{Err: adUnavailable}
	}
	if before == after {
		v.Change = "Il gruppo resterebbe a " + peopleCount(before)
	} else {
		v.Change = "Il gruppo passerebbe da " + strconv.Itoa(before) + " a " + peopleCount(after)
	}
	return v
}

// handleSuggestAttributes: nomi di attributi AD compilati sugli utenti, con
// quanti li hanno e qualche valore, per configurarli senza conoscere AD.
func (s *Server) handleSuggestAttributes(w http.ResponseWriter, r *http.Request) {
	if s.directory == nil {
		s.render(w, http.StatusOK, "ad_suggestions", suggestionsView{Err: adUnavailable})
		return
	}
	stats, err := s.directory.AttributeStats(clipQuery(r.FormValue("name")))
	if err != nil {
		s.render(w, http.StatusOK, "ad_suggestions", suggestionsView{Err: adUnavailable})
		return
	}
	v := suggestionsView{}
	for _, st := range stats {
		text := st.Name + " — " + peopleCount(st.Count)
		if len(st.Examples) > 0 {
			text += " — es. " + strings.Join(st.Examples, ", ")
		}
		v.Items = append(v.Items, suggestion{Value: st.Name, Label: identity.DefaultAttrLabel(st.Name), Text: text, Field: "name"})
	}
	s.render(w, http.StatusOK, "ad_suggestions", v)
}

// ── Suggerimenti da AD ─────────────────────────────────────────────────

// clipQuery limita la ricerca a 64 byte senza spezzare un carattere UTF-8.
func clipQuery(q string) string {
	q = strings.TrimSpace(q)
	if len(q) > 64 {
		q = q[:64]
		for !utf8.ValidString(q) {
			q = q[:len(q)-1]
		}
	}
	return q
}

func (s *Server) suggestValues(attr, q string) suggestionsView {
	if s.directory == nil {
		return suggestionsView{Err: adUnavailable}
	}
	// Solo attributi configurati: i suggerimenti non devono elencare valori di
	// attributi qualsiasi (es. matricole o telefoni).
	attrs, err := s.db.ListAudienceAttributes()
	if err != nil {
		return suggestionsView{Err: "Elenco attributi non disponibile."}
	}
	configured := false
	for _, a := range attrs {
		if strings.EqualFold(a.Name, attr) {
			configured, attr = true, a.Name
		}
	}
	if !configured {
		return suggestionsView{Err: "Attributo non configurato."}
	}
	vals, err := s.directory.AttributeValues(attr)
	if err != nil {
		return suggestionsView{Err: adUnavailable}
	}
	v := suggestionsView{}
	lq := strings.ToLower(q)
	for _, x := range vals {
		if strings.Contains(strings.ToLower(x), lq) {
			v.Items = append(v.Items, suggestion{Value: x, Text: x})
			if len(v.Items) == 20 {
				break
			}
		}
	}
	return v
}

func (s *Server) suggestGroups(q string) suggestionsView {
	if s.directory == nil {
		return suggestionsView{Err: adUnavailable}
	}
	gs, err := s.directory.SearchGroups(q)
	if err != nil {
		return suggestionsView{Err: adUnavailable}
	}
	v := suggestionsView{}
	for _, g := range gs {
		v.Items = append(v.Items, suggestion{Value: g.DN, Label: g.Name, Text: g.Name})
	}
	return v
}

func (s *Server) suggestUsers(q string) suggestionsView {
	if s.directory == nil {
		return suggestionsView{Err: adUnavailable}
	}
	ps, err := s.directory.SearchUsers(q)
	if err != nil {
		return suggestionsView{Err: adUnavailable}
	}
	v := suggestionsView{}
	for _, p := range ps {
		text := p.Username
		if p.Name != "" {
			text = p.Name + " (" + p.Username + ")"
		}
		v.Items = append(v.Items, suggestion{Value: p.Username, Text: text})
	}
	return v
}

func (s *Server) handleSuggestValues(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "ad_suggestions", s.suggestValues(r.FormValue("attr"), clipQuery(r.FormValue("q"))))
}

func (s *Server) handleSuggestGroups(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "ad_suggestions", s.suggestGroups(clipQuery(r.FormValue("q"))))
}

func (s *Server) handleSuggestUsers(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "ad_suggestions", s.suggestUsers(clipQuery(r.FormValue("q"))))
}

// handleSuggest serve il form delle regole: smista per tipo di regola.
func (s *Server) handleSuggest(w http.ResponseWriter, r *http.Request) {
	q := clipQuery(r.FormValue("value"))
	var v suggestionsView
	switch r.FormValue("kind") {
	case audience.KindAttr:
		v = s.suggestValues(r.FormValue("attr"), q)
	case audience.KindADGroup:
		v = s.suggestGroups(q)
	default:
		v = s.suggestUsers(q)
	}
	s.render(w, http.StatusOK, "ad_suggestions", v)
}
