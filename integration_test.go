package main_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"zkill-bot/internal/actions"
	"zkill-bot/internal/enrichment"
	"zkill-bot/internal/killmail"
	"zkill-bot/internal/rules"
)

// TestPipeline_EndToEnd feeds a known killmail JSON fixture through the full
// normalize → enrich → evaluate → dispatch pipeline and asserts expected outputs.
func TestPipeline_EndToEnd(t *testing.T) {
	raw, err := os.ReadFile("testdata/killmail_sample.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// --- Normalize ---
	km, err := killmail.NormalizeFromR2Z2(raw)
	if err != nil {
		t.Fatalf("NormalizeFromR2Z2: %v", err)
	}
	if km.KillmailID != 134435757 {
		t.Errorf("KillmailID: got %d", km.KillmailID)
	}

	// --- Enrich (skip if eve.db not present) ---
	const dbPath = "eve.db"
	if _, statErr := os.Stat(dbPath); statErr == nil {
		enricher, err := enrichment.New(dbPath)
		if err != nil {
			t.Fatalf("enrichment.New: %v", err)
		}
		defer enricher.Close()
		enricher.Enrich(km)

		if km.Enriched == nil {
			t.Fatal("Enriched: nil after enrichment")
		}
		if km.Enriched.VictimShipName == "" {
			t.Error("VictimShipName: expected non-empty after enrichment")
		}
	}

	// --- Evaluate rules (match solo-pvp: solo=false on fixture, use value rule) ---
	f64ptr := func(f float64) *float64 { return &f }
	rf := &rules.RuleFile{
		Mode: rules.ModeMultiMatch,
		Rules: []rules.Rule{
			{
				Name:     "value-match",
				Enabled:  true,
				Priority: 1,
				// fixture totalValue = 627,868,229 (~628M)
				Filter:  rules.FilterNode{ZKBValueMin: f64ptr(100_000_000)},
				Actions: []rules.ActionConfig{{Type: "console"}},
			},
			{
				Name:    "no-match",
				Enabled: true,
				Priority: 2,
				// will not match: requires 1T+
				Filter:  rules.FilterNode{ZKBValueMin: f64ptr(1_000_000_000_000)},
				Actions: []rules.ActionConfig{{Type: "console"}},
			},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("Evaluate: expected 1 match, got %d", len(matches))
	}
	if len(matches) > 0 && matches[0].Rule.Name != "value-match" {
		t.Errorf("matched rule: got %q, want value-match", matches[0].Rule.Name)
	}

	// --- Dispatch with webhook verification ---
	webhookCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := actions.NewDispatcher(srv.Client(), 0, time.Millisecond, time.Millisecond)

	webhookMatches := []rules.RuleMatch{
		{
			Rule: &rules.Rule{Name: "webhook-test"},
			Actions: []rules.ActionConfig{{
				Type: "webhook",
				Args: map[string]interface{}{"url": srv.URL},
			}},
		},
	}
	d.Run(context.Background(), km, webhookMatches)

	if !webhookCalled {
		t.Error("webhook: expected server to be called")
	}
	if d.Counters.Success != 1 {
		t.Errorf("dispatcher.Success: got %d, want 1", d.Counters.Success)
	}
}
