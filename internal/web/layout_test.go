package web

import (
	"reflect"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestMergeOrderKeepsHidden(t *testing.T) {
	got := mergeOrder([]int64{1, 2, 3}, []int64{3, 1, 9, 3}, map[int64]bool{1: true, 3: true})
	if !reflect.DeepEqual(got, []int64{3, 1, 2}) {
		t.Fatalf("mergeOrder = %v", got)
	}
	if got := mergeOrder(nil, nil, nil); got == nil || len(got) != 0 {
		t.Fatalf("vuoto: %#v", got)
	}
}

func layoutDash(cats ...database.CategoryWithApps) database.Dashboard {
	return database.Dashboard{Categories: cats}
}

func cat(id int64, apps ...int64) database.CategoryWithApps {
	c := database.CategoryWithApps{Category: database.Category{ID: id}}
	for _, a := range apps {
		c.Apps = append(c.Apps, database.AppWithGuides{App: database.App{ID: a, CategoryID: id}})
	}
	return c
}

func appIDs(d database.Dashboard) (cats []int64, apps [][]int64) {
	for _, c := range d.Categories {
		cats = append(cats, c.ID)
		var as []int64
		for _, a := range c.Apps {
			as = append(as, a.ID)
		}
		apps = append(apps, as)
	}
	return
}

func TestApplyLayout(t *testing.T) {
	d := layoutDash(cat(1, 11, 12, 13), cat(2, 21))
	applyLayout(&d, database.Layout{Categories: map[int64]int{2: 0}, Apps: map[int64]int{13: 0, 11: 1}})
	cats, apps := appIDs(d)
	if !reflect.DeepEqual(cats, []int64{2, 1}) || !reflect.DeepEqual(apps[1], []int64{13, 11, 12}) {
		t.Fatalf("ordine: %v %v", cats, apps)
	}
}

func favIDs(fs []tileView) (ids []int64, pinned []bool) {
	for _, f := range fs {
		ids = append(ids, f.ID)
		pinned = append(pinned, f.Pinned)
	}
	return
}

func TestFavorites(t *testing.T) {
	d := layoutDash(cat(1, 1, 2, 3, 4), cat(2, 5, 6))
	fs := favorites(d, []int64{1}, map[int64]int{2: 12, 3: 10, 4: 9, 1: 50, 6: 10})
	ids, pinned := favIDs(fs)
	if !reflect.DeepEqual(ids, []int64{1, 2, 3, 6}) || !reflect.DeepEqual(pinned, []bool{true, false, false, false}) {
		t.Fatalf("preferiti: %v %v", ids, pinned)
	}
	for _, f := range fs {
		if f.Prefix != "fav-" || !f.Fav {
			t.Fatalf("riquadro dei preferiti: %+v", f)
		}
	}
}

func TestFavoritesCapAfterReturn(t *testing.T) {
	d := layoutDash(cat(1, 1, 2, 3, 4, 5, 6, 7, 8))
	d.Categories[0].Apps[7].NotForViewer = true // 8: non destinata
	fs := favorites(d, []int64{7, 6, 5, 4, 3, 2, 1, 8}, map[int64]int{8: 99})
	if ids, _ := favIDs(fs); !reflect.DeepEqual(ids, []int64{7, 6, 5, 4, 3, 2}) {
		t.Fatalf("tetto: %v", ids)
	}
	fs = favorites(d, nil, map[int64]int{8: 99})
	if len(fs) != 0 {
		t.Fatalf("non destinata tra i preferiti: %v", fs)
	}
}
