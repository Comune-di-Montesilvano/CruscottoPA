package database

import (
	"testing"
	"time"
)

func TestMediaReferenced(t *testing.T) {
	db := newTestDB(t)
	img := "0123456789abcdef0123456789abcdef.png"
	pdf := "fedcba9876543210fedcba9876543210.pdf"
	inGuide := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.webp"
	if _, err := db.CreateAlert(Alert{Title: "A", Body: "![x](/uploads/guide/" + img + ")", Level: LevelNews, StartsAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	db.CreateGuide(Guide{Title: "P", Kind: GuideKindPDF, File: pdf, Enabled: true})
	db.CreateGuide(Guide{Title: "M", Kind: GuideKindMarkdown, Body: "![y](/uploads/guide/" + inGuide + ")", Enabled: true})
	for name, want := range map[string]bool{img: true, pdf: true, inGuide: true, "00000000000000000000000000000000.png": false} {
		got, err := db.MediaReferenced(name)
		if err != nil || got != want {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
}
