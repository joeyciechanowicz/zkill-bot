package rules

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"zkill-bot/internal/killmail"
)

// FilterNode is a single node in the filter tree. It unmarshals from YAML
// and dispatches to the correct evaluation function.
type FilterNode struct {
	// Composite
	And []*FilterNode `yaml:"and"`
	Or  []*FilterNode `yaml:"or"`
	Not *FilterNode   `yaml:"not"`

	// Attacker volume / kill type
	AttackerCountMin *int  `yaml:"attacker_count_min"`
	AttackerCountMax *int  `yaml:"attacker_count_max"`
	Solo             *bool `yaml:"solo"`
	NPC              *bool `yaml:"npc"`
	Awox             *bool `yaml:"awox"`

	// Location
	SolarSystemID   []int64  `yaml:"solar_system_id"`
	SolarSystemName []string `yaml:"solar_system_name"`
	ZKBLabel        []string `yaml:"zkb_label"`

	// Time
	DayOfWeek  []string    `yaml:"day_of_week"`
	TimeWindow *TimeWindow `yaml:"time_window"`

	// Character / corp / alliance — victim
	VictimCharacterID   []int64 `yaml:"victim_character_id"`
	VictimCorporationID []int64 `yaml:"victim_corporation_id"`
	VictimAllianceID    []int64 `yaml:"victim_alliance_id"`

	// Character / corp / alliance — attackers (any)
	AttackerCharacterID   []int64 `yaml:"attacker_character_id"`
	AttackerCorporationID []int64 `yaml:"attacker_corporation_id"`
	AttackerAllianceID    []int64 `yaml:"attacker_alliance_id"`

	// Ship types / groups
	VictimShipTypeID    []int64 `yaml:"victim_ship_type_id"`
	VictimShipGroupID   []int64 `yaml:"victim_ship_group_id"`
	AttackerShipTypeID  []int64 `yaml:"attacker_ship_type_id"`
	ItemTypeID          []int64 `yaml:"item_type_id"`

	// Capital
	HasCapital *bool `yaml:"has_capital"`

	// TheraWormhole takes a list of solar system names and matches if the kill
	// occurred in one of those systems AND that system has an active Eve Scout
	// wormhole connection.
	TheraWormhole []string `yaml:"thera_wormhole"`

	// Value
	ZKBValueMin *float64 `yaml:"zkb_value_min"`
	ZKBValueMax *float64 `yaml:"zkb_value_max"`
}

// TimeWindow defines a UTC time range filter.
type TimeWindow struct {
	From string `yaml:"from"` // "HH:MM"
	To   string `yaml:"to"`   // "HH:MM"
}

// UnmarshalYAML supports both the short scalar form (single label) and a
// sequence form for ZKBLabel.
func (f *FilterNode) UnmarshalYAML(value *yaml.Node) error {
	// Use an alias type to avoid recursion.
	type plain FilterNode
	return value.Decode((*plain)(f))
}

// matchFilter evaluates the filter node against km, returning true on match.
func matchFilter(km *killmail.Killmail, f *FilterNode) bool {
	// --- Composite ---
	if len(f.And) > 0 {
		for _, child := range f.And {
			if !matchFilter(km, child) {
				return false
			}
		}
		return true
	}
	if len(f.Or) > 0 {
		for _, child := range f.Or {
			if matchFilter(km, child) {
				return true
			}
		}
		return false
	}
	if f.Not != nil {
		return !matchFilter(km, f.Not)
	}

	// --- Leaf filters ---

	if f.Solo != nil && km.ZKB.Solo != *f.Solo {
		return false
	}
	if f.NPC != nil && km.ZKB.NPC != *f.NPC {
		return false
	}
	if f.Awox != nil && km.ZKB.Awox != *f.Awox {
		return false
	}
	if f.AttackerCountMin != nil && km.AttackerCount < *f.AttackerCountMin {
		return false
	}
	if f.AttackerCountMax != nil && km.AttackerCount > *f.AttackerCountMax {
		return false
	}

	if len(f.SolarSystemID) > 0 && !containsInt64(f.SolarSystemID, km.SolarSystemID) {
		return false
	}

	if len(f.SolarSystemName) > 0 {
		name := ""
		if km.Enriched != nil {
			name = km.Enriched.SolarSystemName
		}
		if !containsString(f.SolarSystemName, name) {
			return false
		}
	}

	if len(f.ZKBLabel) > 0 && !anyLabelMatch(f.ZKBLabel, km.ZKB.Labels) {
		return false
	}

	if len(f.DayOfWeek) > 0 {
		day := strings.ToLower(km.KillmailTime.UTC().Weekday().String())
		if !containsString(f.DayOfWeek, day) {
			return false
		}
	}

	if f.TimeWindow != nil {
		if !inTimeWindow(km.KillmailTime.UTC(), f.TimeWindow.From, f.TimeWindow.To) {
			return false
		}
	}

	// Victim filters
	if len(f.VictimCharacterID) > 0 && !containsInt64(f.VictimCharacterID, km.Victim.CharacterID) {
		return false
	}
	if len(f.VictimCorporationID) > 0 && !containsInt64(f.VictimCorporationID, km.Victim.CorporationID) {
		return false
	}
	if len(f.VictimAllianceID) > 0 && !containsInt64(f.VictimAllianceID, km.Victim.AllianceID) {
		return false
	}
	if len(f.VictimShipTypeID) > 0 && !containsInt64(f.VictimShipTypeID, km.Victim.ShipTypeID) {
		return false
	}
	if len(f.VictimShipGroupID) > 0 {
		gid := int64(0)
		if km.Enriched != nil {
			gid = km.Enriched.VictimShipGroupID
		}
		if !containsInt64(f.VictimShipGroupID, gid) {
			return false
		}
	}

	// Attacker filters (any attacker must match)
	if len(f.AttackerCharacterID) > 0 && !anyAttackerMatch(km, func(a *killmail.Participant) bool {
		return containsInt64(f.AttackerCharacterID, a.CharacterID)
	}) {
		return false
	}
	if len(f.AttackerCorporationID) > 0 && !anyAttackerMatch(km, func(a *killmail.Participant) bool {
		return containsInt64(f.AttackerCorporationID, a.CorporationID)
	}) {
		return false
	}
	if len(f.AttackerAllianceID) > 0 && !anyAttackerMatch(km, func(a *killmail.Participant) bool {
		return containsInt64(f.AttackerAllianceID, a.AllianceID)
	}) {
		return false
	}
	if len(f.AttackerShipTypeID) > 0 && !anyAttackerMatch(km, func(a *killmail.Participant) bool {
		return containsInt64(f.AttackerShipTypeID, a.ShipTypeID)
	}) {
		return false
	}

	// Item filter
	if len(f.ItemTypeID) > 0 && !anyItemMatch(km, f.ItemTypeID) {
		return false
	}

	// thera_wormhole rules are triggered by the Eve Scout poller, never by kills.
	if len(f.TheraWormhole) > 0 {
		return false
	}

	// Capital
	if f.HasCapital != nil {
		has := km.Enriched != nil && km.Enriched.HasCapital
		if has != *f.HasCapital {
			return false
		}
	}

	// ZKB value
	if f.ZKBValueMin != nil && km.ZKB.TotalValue < *f.ZKBValueMin {
		return false
	}
	if f.ZKBValueMax != nil && km.ZKB.TotalValue > *f.ZKBValueMax {
		return false
	}

	return true
}

// --- helpers ---

func containsInt64(slice []int64, v int64) bool {
	for _, s := range slice {
		if s == v {
			return true
		}
	}
	return false
}

func containsString(slice []string, v string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}

func anyLabelMatch(filters []string, labels []string) bool {
	for _, f := range filters {
		for _, l := range labels {
			if strings.EqualFold(f, l) {
				return true
			}
		}
	}
	return false
}

func anyAttackerMatch(km *killmail.Killmail, pred func(*killmail.Participant) bool) bool {
	for i := range km.Attackers {
		if pred(&km.Attackers[i]) {
			return true
		}
	}
	return false
}

func anyItemMatch(km *killmail.Killmail, typeIDs []int64) bool {
	for _, item := range km.Items {
		if containsInt64(typeIDs, item.ItemTypeID) {
			return true
		}
	}
	return false
}

func inTimeWindow(t time.Time, from, to string) bool {
	hhmm := func(s string) (int, error) {
		var h, m int
		_, err := fmt.Sscanf(s, "%d:%d", &h, &m)
		return h*60+m, err
	}
	cur := t.Hour()*60 + t.Minute()
	f, err1 := hhmm(from)
	e, err2 := hhmm(to)
	if err1 != nil || err2 != nil {
		return false
	}
	if f <= e {
		return cur >= f && cur < e
	}
	// Wraps midnight
	return cur >= f || cur < e
}
