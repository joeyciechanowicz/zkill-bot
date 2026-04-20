package evescout

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const apiBase = "https://api.eve-scout.com/v2/public/signatures"

// Signature is a single wormhole connection returned by the Eve Scout API.
type Signature struct {
	ID             string  `json:"id"`
	WHType         string  `json:"wh_type"`
	MaxShipSize    string  `json:"max_ship_size"`
	ExpiresAt      string  `json:"expires_at"`
	RemainingHours float64 `json:"remaining_hours"`
	SignatureType  string  `json:"signature_type"`

	OutSystemID   int64  `json:"out_system_id"`
	OutSystemName string `json:"out_system_name"`
	OutSignature  string `json:"out_signature"`

	InSystemID    int64  `json:"in_system_id"`
	InSystemClass string `json:"in_system_class"`
	InSystemName  string `json:"in_system_name"`
	InRegionID    int64  `json:"in_region_id"`
	InRegionName  string `json:"in_region_name"`
	InSignature   string `json:"in_signature"`
}

// Client fetches and caches Eve Scout wormhole signatures.
type Client struct {
	hc *http.Client

	mu      sync.RWMutex
	cache   []Signature
	seen    map[string]bool // signature IDs present in the previous refresh
}

func New(hc *http.Client) *Client {
	return &Client{hc: hc, seen: map[string]bool{}}
}

// StartPoller runs a background goroutine that refreshes the signature cache on
// the given interval until ctx is cancelled. onNew is called for each signature
// whose in_system_name is in watchSystems and whose ID was not present in the
// previous refresh — i.e. it is a newly discovered connection.
func (c *Client) StartPoller(ctx context.Context, interval time.Duration, watchSystems []string, onNew func(Signature)) {
	go func() {
		if err := c.refresh(watchSystems, onNew); err != nil {
			slog.Warn("evescout: initial fetch failed", "error", err)
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if err := c.refresh(watchSystems, onNew); err != nil {
					slog.Warn("evescout: background refresh failed", "error", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Lookup returns all signatures whose in_system_name matches the given solar
// system name (case-insensitive).
func (c *Client) Lookup(systemName string) []Signature {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []Signature
	for _, s := range c.cache {
		if strings.EqualFold(s.InSystemName, systemName) {
			out = append(out, s)
		}
	}
	return out
}

func (c *Client) refresh(watchSystems []string, onNew func(Signature)) error {
	thera, err := c.fetchURL(apiBase + "?system_name=thera")
	if err != nil {
		return err
	}
	turnur, err := c.fetchURL(apiBase + "?system_name=turnur")
	if err != nil {
		return err
	}
	all := append(thera, turnur...)

	newSeen := make(map[string]bool, len(all))
	for _, sig := range all {
		newSeen[sig.ID] = true
	}

	c.mu.Lock()
	prev := c.seen
	c.cache = all
	c.seen = newSeen
	c.mu.Unlock()

	slog.Debug("evescout: cache refreshed", "signatures", len(all))

	if onNew != nil {
		for _, sig := range all {
			isNew := !prev[sig.ID]
			isWatched := ContainsIgnoreCase(watchSystems, sig.InSystemName)
			slog.Debug("evescout: checking signature", "id", sig.ID, "in_system", sig.InSystemName, "is_new", isNew, "is_watched", isWatched)
			if isNew && isWatched {
				onNew(sig)
			}
		}
	}
	return nil
}

func (c *Client) fetchURL(url string) ([]Signature, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "zkill-bot/1.0")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("evescout: %s returned %d", url, resp.StatusCode)
	}

	var sigs []Signature
	if err := json.NewDecoder(resp.Body).Decode(&sigs); err != nil {
		return nil, fmt.Errorf("evescout: decode: %w", err)
	}
	return sigs, nil
}

func ContainsIgnoreCase(slice []string, s string) bool {
	for _, v := range slice {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
