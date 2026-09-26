package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const (
	codexUsageURL          = "https://chatgpt.com/backend-api/wham/usage"
	fiveHourWindowSecs     = int64(5 * time.Hour / time.Second)
	weeklyWindowSecs       = int64(7 * 24 * time.Hour / time.Second)
	minMonthlyWindowSecs   = int64(28 * 24 * time.Hour / time.Second)
	maxMonthlyWindowSecs   = int64(31 * 24 * time.Hour / time.Second)
)

type whamUsage struct {
	PlanType  string        `json:"plan_type"`
	RateLimit whamRateLimit `json:"rate_limit"`
}

type whamRateLimit struct {
	Allowed         *bool        `json:"allowed"`
	LimitReached    *bool        `json:"limit_reached"`
	PrimaryWindow   *quotaWindow `json:"primary_window"`
	SecondaryWindow *quotaWindow `json:"secondary_window"`
}

type quotaWindow struct {
	UsedPercent       *float64 `json:"used_percent"`
	LimitWindowSec    int64    `json:"limit_window_seconds"`
	WindowMinutes     int64    `json:"window_minutes"`
	ResetAfterSeconds int64    `json:"reset_after_seconds"`
	ResetAt           int64    `json:"reset_at"`
}

type quotaWindowSnapshot struct {
	ID          string
	Label       string
	UsedPercent float64
	Remaining   float64
	WindowSecs  int64
	ResetAt     time.Time
	IsFull      bool
}

type quotaSnapshot struct {
	PlanType string
	Windows  []quotaWindowSnapshot
}

func (q quotaSnapshot) fullWindowIDs() []string {
	out := make([]string, 0, len(q.Windows))
	for _, window := range q.Windows {
		if window.IsFull {
			out = append(out, window.ID)
		}
	}
	return out
}

func (q quotaSnapshot) fullWindowLabels() []string {
	out := make([]string, 0, len(q.Windows))
	for _, window := range q.Windows {
		if window.IsFull {
			out = append(out, window.Label)
		}
	}
	return out
}

// fetchQuotaViaCPA always performs a fresh quota probe through CPA host callbacks.
// No locally recorded quota value is used to decide whether an account has a newly
// available quota window.
func fetchQuotaViaCPA(ctx context.Context, cfg pluginConfig, auth pluginapi.HostAuthFileEntry) (quotaSnapshot, error) {
	if strings.TrimSpace(auth.AuthIndex) == "" {
		return quotaSnapshot{}, fmt.Errorf("missing auth_index")
	}
	material, err := getAuthMaterial(ctx, auth.AuthIndex)
	if err != nil {
		return quotaSnapshot{}, err
	}

	headers := http.Header{
		"Authorization": []string{"Bearer " + material.AccessToken},
		"Accept":        []string{"application/json"},
		"User-Agent":    []string{"codex-cli"},
	}
	if material.AccountID != "" {
		headers.Set("ChatGPT-Account-Id", material.AccountID)
	}

	resp, err := hostHTTP(ctx, pluginapi.HTTPRequest{
		Method:  http.MethodGet,
		URL:     codexUsageURL,
		Headers: headers,
	})
	if err != nil {
		return quotaSnapshot{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return quotaSnapshot{}, fmt.Errorf("wham/usage returned HTTP %d: %s", resp.StatusCode, compactBytes(resp.Body, 500))
	}

	var usage whamUsage
	if err := json.Unmarshal(resp.Body, &usage); err != nil {
		return quotaSnapshot{}, fmt.Errorf("decode wham/usage: %w", err)
	}

	windows := trackedQuotaWindows(usage.RateLimit, cfg.FullUsedPercent, time.Now())
	if len(windows) == 0 {
		return quotaSnapshot{}, fmt.Errorf("wham/usage did not contain a usable 5h, weekly, or monthly quota window")
	}

	return quotaSnapshot{
		PlanType: strings.TrimSpace(usage.PlanType),
		Windows:  windows,
	}, nil
}

func trackedQuotaWindows(rate whamRateLimit, threshold float64, now time.Time) []quotaWindowSnapshot {
	type candidate struct {
		position string
		window   *quotaWindow
	}
	candidates := []candidate{
		{position: "primary", window: rate.PrimaryWindow},
		{position: "secondary", window: rate.SecondaryWindow},
	}

	out := make([]quotaWindowSnapshot, 0, len(candidates))
	for _, candidate := range candidates {
		window := candidate.window
		if window == nil || window.UsedPercent == nil {
			continue
		}
		id, label := classifyQuotaWindow(candidate.position, window)
		if id == "" {
			continue
		}
		used := clamp(*window.UsedPercent, 0, 100)
		out = append(out, quotaWindowSnapshot{
			ID:          id,
			Label:       label,
			UsedPercent: used,
			Remaining:   100 - used,
			WindowSecs:  windowDurationSeconds(window),
			ResetAt:     resolveResetAt(window, now),
			IsFull:      quotaFull(rate, window, threshold),
		})
	}
	return out
}

func classifyQuotaWindow(position string, window *quotaWindow) (string, string) {
	seconds := windowDurationSeconds(window)
	switch {
	case seconds == fiveHourWindowSecs:
		return "five-hour", "5h"
	case seconds == weeklyWindowSecs:
		return "weekly", "Weekly"
	case seconds >= minMonthlyWindowSecs && seconds <= maxMonthlyWindowSecs:
		return "monthly", "Monthly"
	}

	// Legacy Codex payloads may omit durations. Preserve the conventional
	// primary/secondary meaning without pretending the secondary is weekly/monthly.
	if seconds == 0 {
		switch position {
		case "primary":
			return "five-hour", "5h"
		case "secondary":
			return "secondary", "Secondary"
		}
	}
	return "", ""
}

func quotaFull(rate whamRateLimit, window *quotaWindow, threshold float64) bool {
	if window == nil || window.UsedPercent == nil {
		return false
	}
	if rate.Allowed != nil && !*rate.Allowed {
		return false
	}
	if rate.LimitReached != nil && *rate.LimitReached {
		return false
	}
	return *window.UsedPercent <= threshold
}

func windowDurationSeconds(window *quotaWindow) int64 {
	if window == nil {
		return 0
	}
	if window.LimitWindowSec > 0 {
		return window.LimitWindowSec
	}
	if window.WindowMinutes > 0 {
		return window.WindowMinutes * 60
	}
	return 0
}

func resolveResetAt(window *quotaWindow, now time.Time) time.Time {
	if window == nil {
		return time.Time{}
	}
	if window.ResetAt > 0 {
		return time.Unix(window.ResetAt, 0).UTC()
	}
	if window.ResetAfterSeconds > 0 {
		return now.UTC().Add(time.Duration(window.ResetAfterSeconds) * time.Second)
	}
	return time.Time{}
}

func clamp(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
