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

// Eliminando un'app le sue guide diventano generali: quelle pubbliche prendono
// la visibilità dell'app, così una guida di un'app riservata non diventa di tutti.
func TestDeleteAppKeepsGuidesRestricted(t *testing.T) {
	db := newTestDB(t)
	ced, _ := db.CreateAudienceGroup("CED")
	altri, _ := db.CreateAudienceGroup("Altri")
	apps, _ := db.ListApps()
	app := apps[0].ID
	db.SetContentAudience(ContentApp, app, ContentAudience{Mode: audience.ModeOnly, Groups: []int64{ced}})
	pub, _ := db.CreateGuide(Guide{AppID: &app, Title: "pubblica", Kind: GuideKindLink, URL: "https://x", Enabled: true})
	own, _ := db.CreateGuide(Guide{AppID: &app, Title: "propria", Kind: GuideKindLink, URL: "https://y", Enabled: true})
	ownCA := ContentAudience{Mode: audience.ModeHide, Groups: []int64{altri}}
	db.SetContentAudience(ContentGuide, own, ownCA)

	if err := db.DeleteApp(app); err != nil {
		t.Fatal(err)
	}
	if ca, _ := db.GetContentAudience(ContentGuide, pub); ca.Mode != audience.ModeOnly || !reflect.DeepEqual(ca.Groups, []int64{ced}) {
		t.Fatalf("guida pubblica: atteso Riservato a CED, ottenuto %+v", ca)
	}
	if ca, _ := db.GetContentAudience(ContentGuide, own); !reflect.DeepEqual(ca, ownCA) {
		t.Fatalf("guida con visibilità propria non deve cambiare: %+v", ca)
	}
}

// Contenuto e visibilità si salvano insieme: se la visibilità non si può
// salvare (gruppo inesistente) non resta un contenuto pubblico a metà.
func TestSaveWithAudienceIsAtomic(t *testing.T) {
	db := newTestDB(t)
	bad := ContentAudience{Mode: audience.ModeOnly, Groups: []int64{999}}
	before, _ := db.ListApps()
	cats, _ := db.ListCategories()
	if _, err := db.CreateAppWithAudience(App{CategoryID: cats[0].ID, Title: "Nuova", IconKind: IconMonogram, IconColor: "#000000"}, bad); err == nil {
		t.Fatal("gruppo inesistente accettato")
	}
	if after, _ := db.ListApps(); len(after) != len(before) {
		t.Fatal("app creata nonostante l'errore sulla visibilità")
	}

	id, _ := db.CreateAlert(Alert{Title: "Prima", Level: LevelNews, StartsAt: time.Now()})
	a, _ := db.GetAlert(id)
	a.Title = "Dopo"
	if err := db.UpdateAlertWithAudience(a, bad); err == nil {
		t.Fatal("gruppo inesistente accettato in aggiornamento")
	}
	if got, _ := db.GetAlert(id); got.Title != "Prima" {
		t.Fatalf("avviso modificato nonostante l'errore: %q", got.Title)
	}

	ced, _ := db.CreateAudienceGroup("CED")
	gid, err := db.CreateGuideWithAudience(Guide{Title: "G", Kind: GuideKindLink, URL: "https://g", Enabled: true}, ContentAudience{Mode: audience.ModeHide, Groups: []int64{ced}})
	if ca, _ := db.GetContentAudience(ContentGuide, gid); err != nil || ca.Mode != audience.ModeHide {
		t.Fatalf("guida con visibilità: %v %+v", err, ca)
	}
}

func TestADGroupRuleNormalized(t *testing.T) {
	db := newTestDB(t)
	g, _ := db.CreateAudienceGroup("G")
	if _, err := db.AddAudienceRule(AudienceRule{GroupID: g, Kind: audience.KindADGroup, Value: "CN=X,OU=Y,DC=z"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddAudienceRule(AudienceRule{GroupID: g, Kind: audience.KindADGroup, Value: "cn=x, ou=y, dc=Z"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("stesso DN scritto diversamente: atteso ErrDuplicate, ottenuto %v", err)
	}
	if _, err := db.AddAudienceRule(AudienceRule{GroupID: g, Kind: audience.KindADGroup, Value: "non un DN"}); !errors.Is(err, ErrInvalidDN) {
		t.Fatalf("DN non valido: atteso ErrInvalidDN, ottenuto %v", err)
	}
}

func TestRequirementRules(t *testing.T) {
	db := newTestDB(t)
	g, _ := db.CreateAudienceGroup("G")
	mail, _ := db.CreateAudienceAttribute("mail", "Email")
	if _, err := db.AddAudienceRule(AudienceRule{GroupID: g, Kind: audience.KindPresent, Attr: "mail", Value: "ignorato"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddAudienceRule(AudienceRule{GroupID: g, Kind: audience.KindPresent, Attr: "mail"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("requisito doppio: %v", err)
	}
	if _, err := db.AddAudienceRule(AudienceRule{GroupID: g, Kind: audience.KindAbsent, Attr: "mail"}); err != nil {
		t.Fatal(err)
	}
	all, _ := db.AllAudienceRules()
	want := []audience.Rule{{Kind: audience.KindPresent, Attr: "mail"}, {Kind: audience.KindAbsent, Attr: "mail"}}
	if !reflect.DeepEqual(all[g], want) {
		t.Fatalf("AllAudienceRules: %v", all[g])
	}
	if err := db.DeleteAudienceAttribute(mail); !errors.Is(err, ErrInUse) {
		t.Fatalf("attributo usato da un requisito: atteso ErrInUse, ottenuto %v", err)
	}
}

func TestAttributeHero(t *testing.T) {
	db := newTestDB(t)
	off, _ := db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	tel, _ := db.CreateAudienceAttribute("telephoneNumber", "Interno")
	mail, _ := db.CreateAudienceAttribute("mail", "Email")
	if err := db.SetAttributeHero(tel, HeroPhone); err != nil {
		t.Fatal(err)
	}
	db.SetAttributeHero(off, HeroText)
	db.SetAttributeHero(mail, HeroMail)
	if err := db.SetAttributeHero(off, "boh"); err == nil {
		t.Fatal("formato non valido accettato")
	}
	db.MoveAttributeHero(off, -1)
	h, _ := db.HeroAttributes()
	if len(h) != 3 || h[0].ID != off || h[1].ID != tel || h[2].HeroKind != HeroMail {
		t.Fatalf("ordine: %+v", h)
	}
	db.SetAttributeHero(off, "")
	h, _ = db.HeroAttributes()
	if len(h) != 2 || h[0].ID != tel || h[0].Hero != 1 || h[1].Hero != 2 {
		t.Fatalf("dopo averne tolto uno: %+v", h)
	}
	if err := db.SetAttributeHero(999, HeroText); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inesistente: %v", err)
	}
}
