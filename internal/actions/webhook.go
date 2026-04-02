package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"zkill-bot/internal/killmail"
)

// discordEmbed holds a single Discord embed object.
type discordEmbed struct {
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	URL         string         `json:"url,omitempty"`
	Color       int            `json:"color"`
	Fields      []embedField   `json:"fields,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
	Footer      *embedFooter   `json:"footer,omitempty"`
	Thumbnail   *embedThumbnail `json:"thumbnail,omitempty"`
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type embedFooter struct {
	Text string `json:"text"`
}

type embedThumbnail struct {
	URL string `json:"url"`
}

type discordWebhookPayload struct {
	Content string         `json:"content,omitempty"`
	Embeds  []discordEmbed `json:"embeds"`
}

// WebhookAction posts a Discord embed to a webhook URL.
type WebhookAction struct {
	client *http.Client
}

// NewWebhookAction constructs a WebhookAction with a shared HTTP client.
func NewWebhookAction(client *http.Client) *WebhookAction {
	return &WebhookAction{client: client}
}

// Execute sends a Discord embed for km to the webhook URL specified in args.
// Supported args:
//
//	"url"      (string)  — required; the Discord webhook URL
//	"template" (string)  — optional; "default", "capital", "loss"
func (w *WebhookAction) Execute(ctx context.Context, km *killmail.Killmail, args map[string]interface{}) error {
	url, _ := args["url"].(string)
	if url == "" {
		return fmt.Errorf("webhook: missing 'url' in action args")
	}
	template, _ := args["template"].(string)

	embed := buildEmbed(km, template)
	payload := discordWebhookPayload{Embeds: []discordEmbed{embed}}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "zkill-bot/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: Discord returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func buildEmbed(km *killmail.Killmail, template string) discordEmbed {
	shipName := fmt.Sprintf("TypeID:%d", km.Victim.ShipTypeID)
	if km.Enriched != nil && km.Enriched.VictimShipName != "" {
		shipName = km.Enriched.VictimShipName
	}

	color := 0xE74C3C // red (kill)
	if template == "loss" {
		color = 0xE67E22 // orange (loss)
	} else if template == "capital" {
		color = 0x9B59B6 // purple (capital)
	}

	title := fmt.Sprintf("%s destroyed", shipName)
	if template == "loss" {
		title = fmt.Sprintf("%s lost", shipName)
	}

	embed := discordEmbed{
		Title:     title,
		URL:       zkbURL(km.KillmailID),
		Color:     color,
		Timestamp: km.KillmailTime.UTC().Format(time.RFC3339),
		Thumbnail: &embedThumbnail{
			URL: fmt.Sprintf("https://images.evetech.net/types/%d/icon?size=64", km.Victim.ShipTypeID),
		},
		Footer: &embedFooter{Text: fmt.Sprintf("Kill #%d", km.KillmailID)},
	}

	embed.Fields = []embedField{
		{Name: "Value", Value: formatISK(km.ZKB.TotalValue), Inline: true},
		{Name: "Attackers", Value: fmt.Sprintf("%d", km.AttackerCount), Inline: true},
		{Name: "System", Value: fmt.Sprintf("%d", km.SolarSystemID), Inline: true},
	}

	// Add attacker ship info for capital template
	if template == "capital" && km.Enriched != nil {
		var capShips []string
		for i, a := range km.Attackers {
			name := fmt.Sprintf("TypeID:%d", a.ShipTypeID)
			if i < len(km.Enriched.AttackerShips) && km.Enriched.AttackerShips[i].TypeName != "" {
				name = km.Enriched.AttackerShips[i].TypeName
			}
			capShips = append(capShips, name)
		}
		if len(capShips) > 0 {
			if len(capShips) > 10 {
				capShips = capShips[:10]
			}
			embed.Fields = append(embed.Fields, embedField{
				Name:  "Attacker Ships",
				Value: strings.Join(capShips, "\n"),
			})
		}
	}

	// Flags line
	var flags []string
	if km.ZKB.Solo {
		flags = append(flags, "Solo")
	}
	if km.ZKB.NPC {
		flags = append(flags, "NPC")
	}
	if km.Enriched != nil && km.Enriched.HasCapital {
		flags = append(flags, "Capital")
	}
	if len(flags) > 0 {
		embed.Fields = append(embed.Fields, embedField{
			Name:  "Tags",
			Value: strings.Join(flags, ", "),
		})
	}

	return embed
}
