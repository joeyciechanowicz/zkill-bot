package actions

import (
	"context"
	"fmt"
	"strings"

	"zkill-bot/internal/killmail"
)

// ConsoleAction prints a human-readable summary of the killmail to stdout.
type ConsoleAction struct{}

func (ConsoleAction) Execute(_ context.Context, km *killmail.Killmail, _ map[string]interface{}) error {
	var sb strings.Builder

	shipName := fmt.Sprintf("TypeID:%d", km.Victim.ShipTypeID)
	if km.Enriched != nil && km.Enriched.VictimShipName != "" {
		shipName = km.Enriched.VictimShipName
	}

	value := formatISK(km.ZKB.TotalValue)

	flags := []string{}
	if km.ZKB.Solo {
		flags = append(flags, "Solo")
	}
	if km.ZKB.NPC {
		flags = append(flags, "NPC")
	}
	if km.ZKB.Awox {
		flags = append(flags, "Awox")
	}
	if km.Enriched != nil && km.Enriched.HasCapital {
		flags = append(flags, "Capital")
	}

	flagStr := ""
	if len(flags) > 0 {
		flagStr = " | " + strings.Join(flags, ", ")
	}

	finalBlowChar := int64(0)
	if km.FinalBlow != nil {
		finalBlowChar = km.FinalBlow.CharacterID
	}

	sb.WriteString(fmt.Sprintf(
		"[%s] Kill #%d | %s | Victim: Corp:%d | Ship: %s | Attackers: %d | Value: %s | System: %d | FinalBlow: %d%s\n",
		km.KillmailTime.Format("2006-01-02T15:04:05Z"),
		km.KillmailID,
		zkbURL(km.KillmailID),
		km.Victim.CorporationID,
		shipName,
		km.AttackerCount,
		value,
		km.SolarSystemID,
		finalBlowChar,
		flagStr,
	))

	fmt.Print(sb.String())
	return nil
}

func zkbURL(killmailID int64) string {
	return fmt.Sprintf("https://zkillboard.com/kill/%d/", killmailID)
}

func formatISK(v float64) string {
	switch {
	case v >= 1_000_000_000_000:
		return fmt.Sprintf("%.1fT ISK", v/1_000_000_000_000)
	case v >= 1_000_000_000:
		return fmt.Sprintf("%.1fB ISK", v/1_000_000_000)
	case v >= 1_000_000:
		return fmt.Sprintf("%.1fM ISK", v/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("%.1fK ISK", v/1_000)
	default:
		return fmt.Sprintf("%.0f ISK", v)
	}
}
