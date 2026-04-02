package actions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"zkill-bot/internal/actions"
	"zkill-bot/internal/killmail"
	"zkill-bot/internal/rules"
	"zkill-bot/internal/state"
)

func newState(t *testing.T) *state.State {
	t.Helper()
	s, err := state.Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func buildKM() *killmail.Killmail {
	return &killmail.Killmail{
		KillmailID:    42,
		SequenceID:    1,
		KillmailTime:  time.Now(),
		SolarSystemID: 30000186,
		AttackerCount: 1,
		Victim:        killmail.Participant{ShipTypeID: 670, CorporationID: 123},
		Attackers:     []killmail.Participant{{ShipTypeID: 37456, FinalBlow: true}},
		ZKB:           killmail.ZKBMeta{TotalValue: 1_000_000},
	}
}

func TestDispatcher_ConsoleAction(t *testing.T) {
	s := newState(t)
	d := actions.NewDispatcher(s, http.DefaultClient, 0, time.Millisecond, time.Millisecond)

	km := buildKM()
	matches := []rules.RuleMatch{
		{
			Rule:    &rules.Rule{Name: "test-rule"},
			Actions: []rules.ActionConfig{{Type: "console"}},
		},
	}

	d.Run(context.Background(), km, matches)

	if d.Counters.Success != 1 {
		t.Errorf("Success: got %d, want 1", d.Counters.Success)
	}
	if d.Counters.Failure != 0 {
		t.Errorf("Failure: got %d, want 0", d.Counters.Failure)
	}
}

func TestDispatcher_IdempotencyPreventsDoubleExecution(t *testing.T) {
	s := newState(t)
	d := actions.NewDispatcher(s, http.DefaultClient, 0, time.Millisecond, time.Millisecond)

	km := buildKM()
	matches := []rules.RuleMatch{
		{
			Rule:    &rules.Rule{Name: "test-rule"},
			Actions: []rules.ActionConfig{{Type: "console"}},
		},
	}

	d.Run(context.Background(), km, matches)
	d.Run(context.Background(), km, matches) // second call should be skipped

	if d.Counters.SkipDupe != 1 {
		t.Errorf("SkipDupe: got %d, want 1", d.Counters.SkipDupe)
	}
	if d.Counters.Success != 1 {
		t.Errorf("Success: got %d, want 1 (not 2)", d.Counters.Success)
	}
}

func TestDispatcher_WebhookAction(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	s := newState(t)
	d := actions.NewDispatcher(s, srv.Client(), 0, time.Millisecond, time.Millisecond)

	km := buildKM()
	matches := []rules.RuleMatch{
		{
			Rule: &rules.Rule{Name: "webhook-rule"},
			Actions: []rules.ActionConfig{{
				Type: "webhook",
				Args: map[string]interface{}{"url": srv.URL},
			}},
		},
	}

	d.Run(context.Background(), km, matches)

	if !called {
		t.Error("webhook: expected server to be called")
	}
	if d.Counters.Success != 1 {
		t.Errorf("Success: got %d, want 1", d.Counters.Success)
	}
}

func TestDispatcher_UnknownActionType(t *testing.T) {
	s := newState(t)
	d := actions.NewDispatcher(s, http.DefaultClient, 0, time.Millisecond, time.Millisecond)

	km := buildKM()
	matches := []rules.RuleMatch{
		{
			Rule:    &rules.Rule{Name: "bad"},
			Actions: []rules.ActionConfig{{Type: "nonexistent"}},
		},
	}

	d.Run(context.Background(), km, matches)

	if d.Counters.Failure != 1 {
		t.Errorf("Failure: got %d, want 1 for unknown action", d.Counters.Failure)
	}
}

func TestDispatcher_WebhookRetry(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	s := newState(t)
	d := actions.NewDispatcher(s, srv.Client(), 3, time.Millisecond, 10*time.Millisecond)

	km := buildKM()
	matches := []rules.RuleMatch{
		{
			Rule: &rules.Rule{Name: "retry-rule"},
			Actions: []rules.ActionConfig{{
				Type: "webhook",
				Args: map[string]interface{}{"url": srv.URL},
			}},
		},
	}

	d.Run(context.Background(), km, matches)

	if d.Counters.Success != 1 {
		t.Errorf("Success: got %d, want 1 (succeeded on retry)", d.Counters.Success)
	}
	if d.Counters.Retry < 1 {
		t.Errorf("Retry: got %d, want >= 1", d.Counters.Retry)
	}
}
