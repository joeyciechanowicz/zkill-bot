package evescout

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	apiBase    = "https://api.eve-scout.com/v2/public/signatures"
	defaultTTL = 5 * time.Minute
)

type signature struct {
	InSystemName  string `json:"in_system_name"`
	OutSystemName string `json:"out_system_name"`
	WHType        string `json:"wh_type"`
	MaxShipSize   string `json:"max_ship_size"`
}

// Client fetches and caches Eve Scout wormhole signatures.
type Client struct {
	hc  *http.Client
	ttl time.Duration

	mu      sync.Mutex
	cache   []signature
	cacheAt time.Time
}

func New(hc *http.Client) *Client {
	return &Client{hc: hc, ttl: defaultTTL}
}

// Lookup returns the out_system_name values for any signature whose
// in_system_name matches the given solar system name (case-insensitive).
func (c *Client) Lookup(systemName string) ([]string, error) {
	sigs, err := c.fetch()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range sigs {
		if strings.EqualFold(s.InSystemName, systemName) {
			out = append(out, s.OutSystemName)
		}
	}
	return out, nil
}

func (c *Client) fetch() ([]signature, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.cacheAt) < c.ttl {
		return c.cache, nil
	}

	thera, err := c.fetchURL(apiBase + "?system_name=thera")
	if err != nil {
		return nil, err
	}
	turnur, err := c.fetchURL(apiBase + "?system_name=turnur")
	if err != nil {
		return nil, err
	}

	all := append(thera, turnur...)
	c.cache = all
	c.cacheAt = time.Now()
	return all, nil
}

func (c *Client) fetchURL(url string) ([]signature, error) {
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

	var sigs []signature
	if err := json.NewDecoder(resp.Body).Decode(&sigs); err != nil {
		return nil, fmt.Errorf("evescout: decode: %w", err)
	}
	return sigs, nil
}
