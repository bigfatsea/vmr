// Ver 2026-07-26, by Sonnet 5
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"vmr/internal/config"
	"vmr/internal/core"
)

func cmdStatus(args []string) error {
	path, err := configFlag(args, "status")
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	// Bare Transport (nil Proxy): this is a local diagnostic call to vmr's
	// own admin endpoint — it must never route through a proxy, and vmr
	// ignores proxy environment variables everywhere by design (§10).
	statusClient := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{}}
	resp, err := statusClient.Get("http://" + cfg.Listen + "/admin/status")
	if err != nil {
		return fmt.Errorf("is vmr running on %s? %w", cfg.Listen, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status endpoint returned %d: %s", resp.StatusCode, body)
	}
	var st struct {
		Models map[string][]struct {
			Endpoint      string    `json:"endpoint"`
			Protocol      string    `json:"protocol"`
			Priority      int       `json:"priority"`
			Fails         int       `json:"consecutive_failures"`
			CooldownUntil time.Time `json:"cooldown_until"`
			LastError     string    `json:"last_error"`
			Available     bool      `json:"available"`
			Probing       bool      `json:"probing"`
		} `json:"models"`
		Concurrency struct {
			Limit    int   `json:"limit"`
			InFlight int64 `json:"in_flight"`
			Waiting  int64 `json:"waiting"`
		} `json:"concurrency"`
	}
	if err := json.Unmarshal(body, &st); err != nil {
		return err
	}
	if st.Concurrency.Limit > 0 {
		fmt.Printf("concurrency: %d/%d in flight, %d waiting\n",
			st.Concurrency.InFlight, st.Concurrency.Limit, st.Concurrency.Waiting)
	}
	for _, name := range core.SortedKeys(st.Models) {
		fmt.Println(name) // key is already "name [protocol]"
		for _, ep := range st.Models[name] {
			state := "ok"
			if !ep.Available {
				state = fmt.Sprintf("COOLDOWN until %s (%s, fails=%d)",
					ep.CooldownUntil.Local().Format("15:04:05"), ep.LastError, ep.Fails)
			} else if ep.Fails > 0 {
				probing := ""
				if ep.Probing {
					probing = ", probing" // a passive-mode real request or an active-mode background probe currently holds this endpoint's single-flight recovery check
				}
				state = fmt.Sprintf("half-open (%s, fails=%d%s)", ep.LastError, ep.Fails, probing)
			}
			fmt.Printf("  p%-3d %-40s %s\n", ep.Priority, ep.Endpoint, state)
		}
	}
	return nil
}
