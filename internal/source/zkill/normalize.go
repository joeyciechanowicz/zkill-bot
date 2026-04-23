package zkill

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/joeyciechanowicz/eve-bot/event"
)

// normalize parses a raw R2Z2 payload into an event.Event. Errors are
// returned for structurally-invalid payloads; callers should drop them.
func normalize(raw []byte) (*event.Event, error) {
	var p r2z2Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	if p.KillmailID == 0 {
		return nil, fmt.Errorf("missing killmail_id")
	}
	if p.SequenceID == 0 {
		return nil, fmt.Errorf("missing sequence_id")
	}
	if len(p.ESI) == 0 {
		return nil, fmt.Errorf("missing esi block")
	}

	var esi r2z2ESI
	if err := json.Unmarshal(p.ESI, &esi); err != nil {
		return nil, fmt.Errorf("unmarshal esi: %w", err)
	}
	if esi.Victim.ShipTypeID == 0 && esi.Victim.CorporationID == 0 {
		return nil, fmt.Errorf("victim has no ship or corporation")
	}

	killmailTime := time.Time{}
	if esi.KillmailTime != "" {
		t, err := time.Parse(time.RFC3339, esi.KillmailTime)
		if err != nil {
			return nil, fmt.Errorf("parse killmail_time %q: %w", esi.KillmailTime, err)
		}
		killmailTime = t
	}

	attackers := make([]any, len(esi.Attackers))
	var finalBlow map[string]any
	for i, a := range esi.Attackers {
		m := map[string]any{
			"character_id":    a.CharacterID,
			"corporation_id":  a.CorporationID,
			"alliance_id":     a.AllianceID,
			"ship_type_id":    a.ShipTypeID,
			"weapon_type_id":  a.WeaponTypeID,
			"damage_done":     a.DamageDone,
			"final_blow":      a.FinalBlow,
			"security_status": a.SecurityStatus,
		}
		attackers[i] = m
		if a.FinalBlow {
			finalBlow = m
		}
	}

	items := make([]any, len(esi.Victim.Items))
	for i, it := range esi.Victim.Items {
		items[i] = map[string]any{
			"item_type_id":       it.ItemTypeID,
			"flag":               it.Flag,
			"quantity_dropped":   it.QuantityDropped,
			"quantity_destroyed": it.QuantityDestroyed,
			"singleton":          it.Singleton,
		}
	}

	victim := map[string]any{
		"character_id":   esi.Victim.CharacterID,
		"corporation_id": esi.Victim.CorporationID,
		"alliance_id":    esi.Victim.AllianceID,
		"ship_type_id":   esi.Victim.ShipTypeID,
		"damage_taken":   esi.Victim.DamageTaken,
	}

	zkb := map[string]any{
		"location_id":     p.ZKB.LocationID,
		"fitted_value":    p.ZKB.FittedValue,
		"dropped_value":   p.ZKB.DroppedValue,
		"destroyed_value": p.ZKB.DestroyedValue,
		"total_value":     p.ZKB.TotalValue,
		"points":          p.ZKB.Points,
		"npc":             p.ZKB.NPC,
		"solo":            p.ZKB.Solo,
		"awox":            p.ZKB.Awox,
		"labels":          toAnySlice(p.ZKB.Labels),
	}

	fields := map[string]any{
		"killmail_id":     p.KillmailID,
		"hash":            p.Hash,
		"sequence_id":     p.SequenceID,
		"uploaded_at":     time.Unix(p.UploadedAt, 0).UTC(),
		"killmail_time":   killmailTime,
		"solar_system_id": esi.SolarSystemID,
		"victim":          victim,
		"attackers":       attackers,
		"attacker_count":  p.ZKB.AttackerCount,
		"final_blow":      finalBlow,
		"items":           items,
		"zkb":             zkb,
	}

	return &event.Event{
		ID:         fmt.Sprintf("zkill:%d", p.KillmailID),
		Source:     "zkill",
		Type:       "killmail",
		OccurredAt: killmailTime,
		Fields:     fields,
	}, nil
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// --- raw R2Z2 JSON shapes (unexported) ---

type r2z2Payload struct {
	KillmailID int64           `json:"killmail_id"`
	Hash       string          `json:"hash"`
	ESI        json.RawMessage `json:"esi"`
	ZKB        r2z2ZKB         `json:"zkb"`
	UploadedAt int64           `json:"uploaded_at"`
	SequenceID int64           `json:"sequence_id"`
}

type r2z2ESI struct {
	KillmailID    int64          `json:"killmail_id"`
	KillmailTime  string         `json:"killmail_time"`
	SolarSystemID int64          `json:"solar_system_id"`
	Attackers     []r2z2Attacker `json:"attackers"`
	Victim        r2z2Victim     `json:"victim"`
}

type r2z2Attacker struct {
	CharacterID    int64   `json:"character_id"`
	CorporationID  int64   `json:"corporation_id"`
	AllianceID     int64   `json:"alliance_id"`
	DamageDone     int64   `json:"damage_done"`
	FinalBlow      bool    `json:"final_blow"`
	SecurityStatus float64 `json:"security_status"`
	ShipTypeID     int64   `json:"ship_type_id"`
	WeaponTypeID   int64   `json:"weapon_type_id"`
}

type r2z2Victim struct {
	CharacterID   int64      `json:"character_id"`
	CorporationID int64      `json:"corporation_id"`
	AllianceID    int64      `json:"alliance_id"`
	ShipTypeID    int64      `json:"ship_type_id"`
	DamageTaken   int64      `json:"damage_taken"`
	Items         []r2z2Item `json:"items"`
}

type r2z2Item struct {
	ItemTypeID        int64 `json:"item_type_id"`
	Flag              int   `json:"flag"`
	QuantityDropped   int64 `json:"quantity_dropped"`
	QuantityDestroyed int64 `json:"quantity_destroyed"`
	Singleton         int   `json:"singleton"`
}

type r2z2ZKB struct {
	LocationID     int64    `json:"locationID"`
	Hash           string   `json:"hash"`
	FittedValue    float64  `json:"fittedValue"`
	DroppedValue   float64  `json:"droppedValue"`
	DestroyedValue float64  `json:"destroyedValue"`
	TotalValue     float64  `json:"totalValue"`
	Points         int      `json:"points"`
	NPC            bool     `json:"npc"`
	Solo           bool     `json:"solo"`
	Awox           bool     `json:"awox"`
	AttackerCount  int      `json:"attackerCount"`
	Labels         []string `json:"labels"`
}
