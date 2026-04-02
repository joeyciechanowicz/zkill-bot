package rules_test

import (
	"testing"
	"time"

	"zkill-bot/internal/killmail"
	"zkill-bot/internal/rules"
)

// buildKM creates a minimal killmail for testing.
func buildKM() *killmail.Killmail {
	return &killmail.Killmail{
		KillmailID:    1,
		SequenceID:    1,
		SolarSystemID: 30000186,
		KillmailTime:  time.Date(2026, 4, 2, 14, 0, 0, 0, time.UTC), // Thursday 14:00 UTC
		AttackerCount: 1,
		Victim: killmail.Participant{
			CharacterID:   100,
			CorporationID: 200,
			AllianceID:    300,
			ShipTypeID:    670,
		},
		Attackers: []killmail.Participant{
			{CharacterID: 999, CorporationID: 888, AllianceID: 777, ShipTypeID: 37456, FinalBlow: true},
		},
		Items: []killmail.Item{
			{ItemTypeID: 27187, QuantityDestroyed: 1},
		},
		ZKB: killmail.ZKBMeta{
			TotalValue: 500_000_000,
			Solo:       false,
			NPC:        false,
			Labels:     []string{"pvp", "loc:lowsec"},
		},
	}
}

func boolPtr(b bool) *bool    { return &b }
func intPtr(i int) *int       { return &i }
func f64Ptr(f float64) *float64 { return &f }

func TestEvaluate_FirstMatch(t *testing.T) {
	km := buildKM()
	km.ZKB.Solo = true

	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "solo", Enabled: true, Priority: 1,
				Filter:  rules.FilterNode{Solo: boolPtr(true)},
				Actions: []rules.ActionConfig{{Type: "console"}}},
			{Name: "all", Enabled: true, Priority: 2,
				Filter:  rules.FilterNode{NPC: boolPtr(false)},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("first-match: expected 1 match, got %d", len(matches))
	}
	if matches[0].Rule.Name != "solo" {
		t.Errorf("first-match: expected rule 'solo', got %q", matches[0].Rule.Name)
	}
}

func TestEvaluate_MultiMatch(t *testing.T) {
	km := buildKM()
	km.ZKB.Solo = true

	rf := &rules.RuleFile{
		Mode: rules.ModeMultiMatch,
		Rules: []rules.Rule{
			{Name: "solo", Enabled: true, Priority: 1,
				Filter:  rules.FilterNode{Solo: boolPtr(true)},
				Actions: []rules.ActionConfig{{Type: "console"}}},
			{Name: "all", Enabled: true, Priority: 2,
				Filter:  rules.FilterNode{NPC: boolPtr(false)},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 2 {
		t.Errorf("multi-match: expected 2 matches, got %d", len(matches))
	}
}

func TestEvaluate_DisabledRuleSkipped(t *testing.T) {
	km := buildKM()
	rf := &rules.RuleFile{
		Mode: rules.ModeMultiMatch,
		Rules: []rules.Rule{
			{Name: "disabled", Enabled: false, Priority: 1,
				Filter:  rules.FilterNode{NPC: boolPtr(false)},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 0 {
		t.Errorf("expected 0 matches for disabled rule, got %d", len(matches))
	}
}

func TestFilter_ZKBValue(t *testing.T) {
	km := buildKM()
	km.ZKB.TotalValue = 2_000_000_000

	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "high-value", Enabled: true, Priority: 1,
				Filter:  rules.FilterNode{ZKBValueMin: f64Ptr(1_000_000_000)},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("zkb_value_min: expected match, got %d matches", len(matches))
	}

	km.ZKB.TotalValue = 500_000_000
	matches = rules.Evaluate(km, rf)
	if len(matches) != 0 {
		t.Errorf("zkb_value_min: expected no match below threshold, got %d", len(matches))
	}
}

func TestFilter_VictimCorp(t *testing.T) {
	km := buildKM()
	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "corp", Enabled: true, Priority: 1,
				Filter:  rules.FilterNode{VictimCorporationID: []int64{200}},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("victim_corporation_id: expected 1 match, got %d", len(matches))
	}
}

func TestFilter_AndComposite(t *testing.T) {
	km := buildKM()
	km.ZKB.Solo = true

	trueFilter := rules.FilterNode{Solo: boolPtr(true)}
	falseFilter := rules.FilterNode{NPC: boolPtr(true)} // km.NPC=false, so this won't match

	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "and-match", Enabled: true, Priority: 1,
				Filter: rules.FilterNode{And: []*rules.FilterNode{&trueFilter, &falseFilter}},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 0 {
		t.Errorf("and: expected 0 matches when one child fails, got %d", len(matches))
	}

	trueFilter2 := rules.FilterNode{NPC: boolPtr(false)}
	rf.Rules[0].Filter = rules.FilterNode{And: []*rules.FilterNode{&trueFilter, &trueFilter2}}
	matches = rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("and: expected 1 match when all children pass, got %d", len(matches))
	}
}

func TestFilter_OrComposite(t *testing.T) {
	km := buildKM()

	neverMatch := rules.FilterNode{NPC: boolPtr(true)}  // NPC=false on km
	alwaysMatch := rules.FilterNode{Solo: boolPtr(false)} // Solo=false on km

	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "or", Enabled: true, Priority: 1,
				Filter: rules.FilterNode{Or: []*rules.FilterNode{&neverMatch, &alwaysMatch}},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("or: expected 1 match when at least one child passes, got %d", len(matches))
	}
}

func TestFilter_NotComposite(t *testing.T) {
	km := buildKM()

	inner := rules.FilterNode{NPC: boolPtr(true)} // NPC=false on km, so inner=false, not(inner)=true
	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "not", Enabled: true, Priority: 1,
				Filter: rules.FilterNode{Not: &inner},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("not: expected 1 match, got %d", len(matches))
	}
}

func TestFilter_AttackerCorp(t *testing.T) {
	km := buildKM()
	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "atk-corp", Enabled: true, Priority: 1,
				Filter:  rules.FilterNode{AttackerCorporationID: []int64{888}},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("attacker_corporation_id: expected 1 match, got %d", len(matches))
	}

	rf.Rules[0].Filter.AttackerCorporationID = []int64{999}
	matches = rules.Evaluate(km, rf)
	if len(matches) != 0 {
		t.Errorf("attacker_corporation_id: expected 0 matches for non-existent corp, got %d", len(matches))
	}
}

func TestFilter_ZKBLabel(t *testing.T) {
	km := buildKM() // has labels: ["pvp", "loc:lowsec"]
	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "label", Enabled: true, Priority: 1,
				Filter:  rules.FilterNode{ZKBLabel: []string{"loc:lowsec"}},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("zkb_label: expected 1 match for loc:lowsec, got %d", len(matches))
	}

	rf.Rules[0].Filter.ZKBLabel = []string{"loc:nullsec"}
	matches = rules.Evaluate(km, rf)
	if len(matches) != 0 {
		t.Errorf("zkb_label: expected 0 matches for missing label, got %d", len(matches))
	}
}

func TestFilter_ItemTypeID(t *testing.T) {
	km := buildKM() // has item 27187
	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "item", Enabled: true, Priority: 1,
				Filter:  rules.FilterNode{ItemTypeID: []int64{27187}},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("item_type_id: expected match, got %d", len(matches))
	}
}

func TestFilter_TimeWindow(t *testing.T) {
	km := buildKM() // KillmailTime = 14:00 UTC
	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "tw", Enabled: true, Priority: 1,
				Filter: rules.FilterNode{TimeWindow: &rules.TimeWindow{From: "13:00", To: "15:00"}},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("time_window: expected match at 14:00 within 13-15, got %d", len(matches))
	}

	rf.Rules[0].Filter.TimeWindow = &rules.TimeWindow{From: "20:00", To: "22:00"}
	matches = rules.Evaluate(km, rf)
	if len(matches) != 0 {
		t.Errorf("time_window: expected no match at 14:00 outside 20-22, got %d", len(matches))
	}
}

func TestFilter_DayOfWeek(t *testing.T) {
	km := buildKM() // KillmailTime = Thursday
	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "day", Enabled: true, Priority: 1,
				Filter:  rules.FilterNode{DayOfWeek: []string{"thursday"}},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("day_of_week: expected match on Thursday, got %d", len(matches))
	}

	rf.Rules[0].Filter.DayOfWeek = []string{"monday"}
	matches = rules.Evaluate(km, rf)
	if len(matches) != 0 {
		t.Errorf("day_of_week: expected no match for wrong day, got %d", len(matches))
	}
}

func TestFilter_SolarSystemName(t *testing.T) {
	km := buildKM()
	km.Enriched = &killmail.EnrichedData{SolarSystemName: "Jita"}

	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "jita", Enabled: true, Priority: 1,
				Filter:  rules.FilterNode{SolarSystemName: []string{"Jita"}},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Errorf("solar_system_name: expected match for Jita, got %d", len(matches))
	}

	rf.Rules[0].Filter.SolarSystemName = []string{"Amarr"}
	matches = rules.Evaluate(km, rf)
	if len(matches) != 0 {
		t.Errorf("solar_system_name: expected no match for wrong system, got %d", len(matches))
	}
}

func TestEvaluate_PriorityOrder(t *testing.T) {
	km := buildKM()
	km.ZKB.Solo = true

	// Define rules in reverse priority order to verify sorting.
	rf := &rules.RuleFile{
		Mode: rules.ModeFirstMatch,
		Rules: []rules.Rule{
			{Name: "low-priority", Enabled: true, Priority: 99,
				Filter:  rules.FilterNode{Solo: boolPtr(true)},
				Actions: []rules.ActionConfig{{Type: "console"}}},
			{Name: "high-priority", Enabled: true, Priority: 1,
				Filter:  rules.FilterNode{Solo: boolPtr(true)},
				Actions: []rules.ActionConfig{{Type: "console"}}},
		},
	}

	matches := rules.Evaluate(km, rf)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Rule.Name != "high-priority" {
		t.Errorf("expected high-priority rule to win, got %q", matches[0].Rule.Name)
	}
}
