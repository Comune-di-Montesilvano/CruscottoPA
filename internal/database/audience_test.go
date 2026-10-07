package database

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
)

func TestAudienceAttributes(t *testing.T) {
	db := newTestDB(t)
	id, err := db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAudienceAttribute("PHYSICALDELIVERYOFFICENAME", "Doppio"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("attributo doppio: %v", err)
	}
	g, _ := db.CreateAudienceGroup("CED")
	db.AddAudienceRule(AudienceRule{GroupID: g, Kind: audience.KindAttr, Attr: "physicalDeliveryOfficeName", Value: "INFORMATIZZAZIONE"})
	if err := db.DeleteAudienceAttribute(id); !errors.Is(err, ErrInUse) {
		t.Fatalf("attributo in uso eliminato: %v", err)
	}
}

func TestAudienceGroupsAndRules(t *testing.T) {
	db := newTestDB(t)
	ced, _ := db.CreateAudienceGroup("CED")
	rag, _ := db.CreateAudienceGroup("Ragioneria")
	if _, err := db.CreateAudienceGroup("ced"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("nome doppio: %v", err)
	}
	r1, err := db.AddAudienceRule(AudienceRule{GroupID: ced, Kind: audience.KindUser, Value: "Mario.Rossi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddAudienceRule(AudienceRule{GroupID: ced, Kind: audience.KindUser, Value: "mario.rossi"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("regola doppia: %v", err)
	}
	if _, err := db.AddAudienceRule(AudienceRule{GroupID: ced, Kind: "boh", Value: "x"}); err == nil {
		t.Fatal("tipo di regola non valido accettato")
	}
	all, _ := db.AllAudienceRules()
	if !reflect.DeepEqual(all[ced], []audience.Rule{{Kind: audience.KindUser, Value: "mario.rossi"}}) || len(all[rag]) != 0 {
		t.Fatalf("AllAudienceRules: %v", all)
	}
	groups, _ := db.ListAudienceGroups()
	if len(groups) != 2 || groups[0].Name != "CED" || groups[0].Rules != 1 {
		t.Fatalf("ListAudienceGroups: %+v", groups)
	}
	if err := db.MoveAudienceGroup(rag, -1); err != nil {
		t.Fatal(err)
	}
	if groups, _ = db.ListAudienceGroups(); groups[0].Name != "Ragioneria" {
		t.Fatalf("spostamento: %+v", groups)
	}
	if err := db.DeleteAudienceRule(ced, r1); err != nil {
		t.Fatal(err)
	}
	if rules, _ := db.ListAudienceRules(ced); len(rules) != 0 {
		t.Fatalf("regola non tolta: %v", rules)
	}
}

func TestContentAudience(t *testing.T) {
	db := newTestDB(t)
	ced, _ := db.CreateAudienceGroup("CED")
	apps, _ := db.ListApps()
	app := apps[0].ID

	if ca, err := db.GetContentAudience(ContentApp, app); err != nil || ca.Mode != audience.ModePublic || len(ca.Groups) != 0 {
		t.Fatalf("iniziale: %+v %v", ca, err)
	}
	want := ContentAudience{Mode: audience.ModeOnly, Groups: []int64{ced}}
	if err := db.SetContentAudience(ContentApp, app, want); err != nil {
		t.Fatal(err)
	}
	if ca, _ := db.GetContentAudience(ContentApp, app); !reflect.DeepEqual(ca, want) {
		t.Fatalf("Get: %+v", ca)
	}
	if m, _ := db.AllContentAudience(ContentApp); !reflect.DeepEqual(m[app], want) {
		t.Fatalf("All: %+v", m)
	}
	if uses, _ := db.GroupUses(ced); len(uses) != 1 {
		t.Fatalf("GroupUses: %v", uses)
	}
	if err := db.DeleteAudienceGroup(ced); !errors.Is(err, ErrInUse) {
		t.Fatalf("gruppo in uso eliminato: %v", err)
	}
	// Eliminare il contenuto toglie le sue righe: il gruppo torna eliminabile.
	if err := db.DeleteApp(app); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteAudienceGroup(ced); err != nil {
		t.Fatalf("gruppo non più in uso: %v", err)
	}

	id, _ := db.CreateAlert(Alert{Title: "x", Level: LevelNews, StartsAt: time.Now()})
	g2, _ := db.CreateAudienceGroup("G2")
	db.SetContentAudience(ContentAlert, id, ContentAudience{Mode: audience.ModeHide, Groups: []int64{g2}})
	db.SetContentAudience(ContentAlert, id, ContentAudience{Mode: audience.ModePublic})
	if ca, _ := db.GetContentAudience(ContentAlert, id); ca.Mode != audience.ModePublic {
		t.Fatalf("ritorno a pubblico: %+v", ca)
	}
}
