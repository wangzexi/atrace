package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A collector reads a list newest first and stops where it meets what is stored, so the question it asks
// for an item has to name the activity that storing the item leaves. The recorded documents of every
// collector are stored, and each activity among them is asked about the way a collector asks (the same
// fields as activityKey in collectors/*/index.mjs): it must be known. A key that missed would not fail
// anything; the collector would read the whole list every time.
func TestEveryRecordedActivityIsKnownOnceStored(t *testing.T) {
	files, err := filepath.Glob("../../client/app/tests/collectors/*/testdata/*.json")
	if err != nil || len(files) < 40 {
		t.Fatalf("fixtures: %d files, %v", len(files), err)
	}
	s := openTest(t)
	type recorded struct {
		Expected []struct {
			Role     string
			Document map[string]any
		}
	}
	var queries []KnownQuery
	var origins []string
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var fixture recorded
		if json.Unmarshal(raw, &fixture) != nil {
			continue // not a list of documents (the emoji maps and the like)
		}
		for _, entry := range fixture.Expected {
			document, _ := json.Marshal(entry.Document)
			if _, err := s.Ingest(context.Background(), input(string(document))); err != nil {
				t.Fatalf("%s: storing %s: %v", file, document, err)
			}
			kind, _ := entry.Document["type"].(string)
			switch kind {
			case "Create", "Like", "Add", "Announce":
			default:
				continue
			}
			query := KnownQuery{Kind: kind}
			query.Actor, _ = entry.Document["actor"].(string)
			query.Object, _ = entry.Document["object"].(string)
			query.Target, _ = entry.Document["target"].(string)
			if kind == "Announce" {
				query.ID, _ = entry.Document["id"].(string)
			}
			queries = append(queries, query)
			origins = append(origins, filepath.Base(filepath.Dir(filepath.Dir(file)))+"/"+filepath.Base(file))
		}
	}
	if len(queries) < 20 {
		t.Fatalf("only %d recorded activities", len(queries))
	}
	known, err := s.Known(context.Background(), queries)
	if err != nil {
		t.Fatal(err)
	}
	for i, ok := range known {
		if !ok {
			t.Errorf("%s: %+v is stored but not known", origins[i], queries[i])
		}
	}
	// And what was never stored is not known.
	other, err := s.Known(context.Background(), []KnownQuery{{Actor: queries[0].Actor, Kind: queries[0].Kind, Object: queries[0].Object + "-never-stored", Target: queries[0].Target}})
	if err != nil || other[0] {
		t.Errorf("an activity that was never stored is known: %v %v", other, err)
	}
}
