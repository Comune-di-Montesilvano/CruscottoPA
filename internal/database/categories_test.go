package database

import (
	"errors"
	"testing"
)

func names(cs []Category) string {
	s := ""
	for _, c := range cs {
		s += c.Name + ","
	}
	return s
}

func TestCategoryCRUD(t *testing.T) {
	db := newTestDB(t)

	id, err := db.CreateCategory("Gestionali esterni")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateCategory("gestionali ESTERNI"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("nome duplicato (case-insensitive): atteso ErrDuplicate, ottenuto %v", err)
	}
	if err := db.UpdateCategory(id, "Esterni"); err != nil {
		t.Fatal(err)
	}
	c, err := db.GetCategory(id)
	if err != nil || c.Name != "Esterni" || c.SortOrder != 1 {
		t.Fatalf("GetCategory: %+v %v", c, err)
	}
	if err := db.UpdateCategory(9999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update inesistente: %v", err)
	}
	if err := db.DeleteCategory(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetCategory(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dopo delete: %v", err)
	}
}

func TestDeleteCategoryWithAppsRefused(t *testing.T) {
	db := newTestDB(t)
	cats, _ := db.ListCategories() // seed: "Applicativi" con 2 app
	if err := db.DeleteCategory(cats[0].ID); !errors.Is(err, ErrCategoryNotEmpty) {
		t.Fatalf("atteso ErrCategoryNotEmpty, ottenuto %v", err)
	}
}

func TestMoveCategory(t *testing.T) {
	db := newTestDB(t)
	b, _ := db.CreateCategory("B")
	db.CreateCategory("C")

	if err := db.MoveCategory(b, -1); err != nil {
		t.Fatal(err)
	}
	cs, _ := db.ListCategories()
	if got := names(cs); got != "B,Applicativi,C," {
		t.Fatalf("dopo su: %s", got)
	}
	// Già in cima: nessun effetto, nessun errore.
	if err := db.MoveCategory(b, -1); err != nil {
		t.Fatal(err)
	}
	if err := db.MoveCategory(9999, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("move inesistente: %v", err)
	}
}
