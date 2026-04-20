package killmail

import (
	"encoding/json"
	"fmt"
	"time"

	"zkill-bot/internal/evescout"
)

// Killmail is the canonical internal representation of a killmail event.
// It is populated in stages: normalization fills core fields; enrichment fills Enriched.
type Killmail struct {
	KillmailID   int64
	Hash         string
	SequenceID   int64
	UploadedAt   time.Time
	KillmailTime time.Time
	SolarSystemID int64

	Victim        Participant
	Attackers     []Participant
	AttackerCount int
	FinalBlow     *Participant // pointer into Attackers slice

	Items []Item

	ZKB ZKBMeta

	Enriched *EnrichedData
}

// Participant represents either the victim or one attacker in a killmail.
type Participant struct {
	CharacterID    int64
	CorporationID  int64
	AllianceID     int64
	ShipTypeID     int64
	WeaponTypeID   int64
	DamageDone     int64
	DamageTaken    int64
	FinalBlow      bool
	SecurityStatus float64
}

// Item is a fitting or cargo entry on the victim.
type Item struct {
	ItemTypeID        int64
	Flag              int
	QuantityDropped   int64
	QuantityDestroyed int64
	Singleton         int
}

// ZKBMeta holds zKillboard-specific metadata attached to each killmail.
type ZKBMeta struct {
	LocationID     int64
	FittedValue    float64
	DroppedValue   float64
	DestroyedValue float64
	TotalValue     float64
	Points         int
	NPC            bool
	Solo           bool
	Awox           bool
	Labels         []string
}

// EnrichedData is populated by the enrichment package after normalization.
type EnrichedData struct {
	VictimShipName     string
	VictimShipGroup    string
	VictimShipCategory string
	VictimShipGroupID  int64
	VictimShipCatID    int64

	AttackerShips []ShipInfo
	ItemNames     []string // parallel slice to Killmail.Items

	HasCapital      bool
	SolarSystemName string

	// WormholeConnections lists Eve Scout signatures whose in_system_name
	// matches this kill's solar system.
	WormholeConnections []evescout.Signature
}

// ShipInfo holds enriched type data for a single ship.
type ShipInfo struct {
	TypeName     string
	GroupName    string
	CategoryName string
	GroupID      int64
	CategoryID   int64
	MetaLevel    int
	MetaGroupID  int64
	MetaGroup    string
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

// NormalizeFromR2Z2 parses a raw R2Z2 JSON payload into a Killmail.
// Returns an error for any payload that is structurally malformed or missing
// required identity fields; callers should skip and continue on error.
func NormalizeFromR2Z2(raw []byte) (*Killmail, error) {
	var p r2z2Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("killmail: unmarshal r2z2 payload: %w", err)
	}

	if p.KillmailID == 0 {
		return nil, fmt.Errorf("killmail: missing killmail_id")
	}
	if p.SequenceID == 0 {
		return nil, fmt.Errorf("killmail: missing sequence_id")
	}
	if len(p.ESI) == 0 {
		return nil, fmt.Errorf("killmail: missing esi block")
	}

	var esi r2z2ESI
	if err := json.Unmarshal(p.ESI, &esi); err != nil {
		return nil, fmt.Errorf("killmail: unmarshal esi block: %w", err)
	}

	if esi.Victim.ShipTypeID == 0 && esi.Victim.CorporationID == 0 {
		return nil, fmt.Errorf("killmail: victim has no ship or corporation")
	}

	km := &Killmail{
		KillmailID:    p.KillmailID,
		Hash:          p.Hash,
		SequenceID:    p.SequenceID,
		UploadedAt:    time.Unix(p.UploadedAt, 0).UTC(),
		SolarSystemID: esi.SolarSystemID,
		AttackerCount: p.ZKB.AttackerCount,
	}

	if esi.KillmailTime != "" {
		t, err := time.Parse(time.RFC3339, esi.KillmailTime)
		if err != nil {
			return nil, fmt.Errorf("killmail: parse killmail_time %q: %w", esi.KillmailTime, err)
		}
		km.KillmailTime = t
	}

	// Victim
	km.Victim = Participant{
		CharacterID:   esi.Victim.CharacterID,
		CorporationID: esi.Victim.CorporationID,
		AllianceID:    esi.Victim.AllianceID,
		ShipTypeID:    esi.Victim.ShipTypeID,
		DamageTaken:   esi.Victim.DamageTaken,
	}

	// Items
	km.Items = make([]Item, len(esi.Victim.Items))
	for i, it := range esi.Victim.Items {
		km.Items[i] = Item{
			ItemTypeID:        it.ItemTypeID,
			Flag:              it.Flag,
			QuantityDropped:   it.QuantityDropped,
			QuantityDestroyed: it.QuantityDestroyed,
			Singleton:         it.Singleton,
		}
	}

	// Attackers
	km.Attackers = make([]Participant, len(esi.Attackers))
	for i, a := range esi.Attackers {
		km.Attackers[i] = Participant{
			CharacterID:    a.CharacterID,
			CorporationID:  a.CorporationID,
			AllianceID:     a.AllianceID,
			ShipTypeID:     a.ShipTypeID,
			WeaponTypeID:   a.WeaponTypeID,
			DamageDone:     a.DamageDone,
			FinalBlow:      a.FinalBlow,
			SecurityStatus: a.SecurityStatus,
		}
		if a.FinalBlow {
			km.FinalBlow = &km.Attackers[i]
		}
	}

	// ZKB
	km.ZKB = ZKBMeta{
		LocationID:     p.ZKB.LocationID,
		FittedValue:    p.ZKB.FittedValue,
		DroppedValue:   p.ZKB.DroppedValue,
		DestroyedValue: p.ZKB.DestroyedValue,
		TotalValue:     p.ZKB.TotalValue,
		Points:         p.ZKB.Points,
		NPC:            p.ZKB.NPC,
		Solo:           p.ZKB.Solo,
		Awox:           p.ZKB.Awox,
		Labels:         p.ZKB.Labels,
	}

	return km, nil
}
