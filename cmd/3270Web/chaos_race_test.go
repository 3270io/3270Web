package main

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/chaos"
)

// TestGetLoadedRunReturnsCloneNotSharedPointer guards against
// getLoadedRun handing back the store's live *SavedRun/*MindMap. A caller
// (ChaosScreensListHandler, ChaosExportHandler, status/marshal paths, etc.)
// then ranges over or json.Marshals that MindMap's Areas/BusinessFunctions
// maps outside any lock; a concurrent business-annotation write (which
// mutates those same maps under the store lock via withLoadedRun) racing
// that unlocked read is a "concurrent map read and map write" — a fatal,
// unrecoverable crash, not a recoverable error.
func TestGetLoadedRunReturnsCloneNotSharedPointer(t *testing.T) {
	store := newChaosEngineStore()
	run := &chaos.SavedRun{
		SavedRunMeta: chaos.SavedRunMeta{ID: "run-1"},
		MindMap: &chaos.MindMap{
			Areas: map[string]*chaos.MindMapArea{
				"h1": {Hash: "h1", Label: "Login"},
			},
		},
	}
	store.setLoadedRun("sess-1", run)

	loaded, ok := store.getLoadedRun("sess-1")
	if !ok {
		t.Fatal("getLoadedRun: run not found")
	}
	if loaded == run {
		t.Fatal("getLoadedRun returned the store's shared *SavedRun instead of a clone")
	}
	if loaded.MindMap == run.MindMap {
		t.Fatal("getLoadedRun's MindMap aliases the store's shared MindMap")
	}
	if loaded.ID != run.ID || loaded.MindMap.Areas["h1"].Label != "Login" {
		t.Fatalf("clone diverged from source: %+v", loaded)
	}
}

// TestGetLoadedRunDoesNotRaceConcurrentBusinessWrite exercises the actual
// concurrent-access scenario under -race: one goroutine repeatedly calls
// getLoadedRun and ranges/marshals the returned MindMap (mirroring
// ChaosScreensListHandler/ChaosStatusHandler/ChaosExportHandler), while
// another repeatedly annotates the loaded run through the store's own
// locked mutation path (mirroring applyChaosBusinessWrite). Before
// getLoadedRun cloned its result, this would race: the reader's iteration
// over a shared, live map could run concurrently with the writer's
// map-header mutation on the very same map.
func TestGetLoadedRunDoesNotRaceConcurrentBusinessWrite(t *testing.T) {
	store := newChaosEngineStore()
	sessID := "sess-race"
	store.setLoadedRun(sessID, &chaos.SavedRun{
		SavedRunMeta: chaos.SavedRunMeta{ID: "run-race"},
		MindMap:      &chaos.MindMap{Areas: map[string]*chaos.MindMapArea{}},
	})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			found, err := store.withLoadedRun(sessID, func(run *chaos.SavedRun) error {
				return chaos.AnnotateSavedRun(run, "h1", "purpose", "", nil)
			})
			if !found {
				t.Error("withLoadedRun: run not found")
				return
			}
			if err != nil {
				t.Errorf("annotate: %v", err)
				return
			}
		}
		close(stop)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			loaded, ok := store.getLoadedRun(sessID)
			if !ok {
				t.Error("getLoadedRun: run not found")
				return
			}
			// CloneRun's MindMap.clone() returns nil for an empty mind map
			// (no areas yet) — that's expected before the writer's first
			// annotation lands, not a bug in this test.
			if loaded.MindMap == nil {
				continue
			}
			// Mirror what handlers actually do with the result: iterate
			// the map and marshal it.
			for range loaded.MindMap.Areas {
			}
			if _, err := json.Marshal(loaded.MindMap); err != nil {
				t.Errorf("marshal loaded mind map: %v", err)
				return
			}
			time.Sleep(time.Microsecond)
		}
	}()

	wg.Wait()
}
