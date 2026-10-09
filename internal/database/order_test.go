package database

import (
	"fmt"
	"sync"
	"testing"
)

// Spostamenti concorrenti: con transazioni DEFERRED due moveRow che hanno già
// letto finiscono in SQLITE_BUSY_SNAPSHOT invece di aspettare busy_timeout.
func TestMoveRowConcurrent(t *testing.T) {
	db := newTestDB(t)
	var ids []int64
	for i := range 6 {
		id, err := db.CreateCategory(fmt.Sprintf("Categoria %d", i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 25 {
				dir := 1
				if (g+i)%2 == 0 {
					dir = -1
				}
				if err := db.MoveCategory(ids[(g+i)%len(ids)], dir); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("MoveCategory concorrente: %v", err)
	}
}
