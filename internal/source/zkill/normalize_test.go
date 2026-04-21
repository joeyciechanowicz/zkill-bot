package zkill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeSample(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "killmail_sample.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ev, err := normalize(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	if ev.ID != "zkill:134435757" {
		t.Errorf("ID: %q", ev.ID)
	}
	if ev.Source != "zkill" || ev.Type != "killmail" {
		t.Errorf("source/type: %q/%q", ev.Source, ev.Type)
	}
	if ev.Fields["killmail_id"].(int64) != 134435757 {
		t.Errorf("killmail_id: %v", ev.Fields["killmail_id"])
	}

	zkb, ok := ev.Fields["zkb"].(map[string]any)
	if !ok {
		t.Fatal("zkb missing")
	}
	if zkb["total_value"].(float64) == 0 {
		t.Error("total_value is zero")
	}
}

func TestEnrichPopulatesShipName(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "killmail_sample.json")
	raw, _ := os.ReadFile(path)
	ev, err := normalize(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	enrich(ev)

	victim := ev.Fields["victim"].(map[string]any)
	if victim["ship_name"] == "" || victim["ship_name"] == nil {
		t.Errorf("victim ship_name empty: %+v", victim)
	}
	if _, ok := ev.Fields["has_capital"].(bool); !ok {
		t.Error("has_capital missing")
	}
}
