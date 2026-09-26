package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const (
	codexUsageURL         = "https://chatgpt.com/backend-api/wham/usage"
	targetShortWindowSecs = int64(5 * time.Hour / time.Second)
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

type quotaSnapshot struct {
	PlanType    string
	UsedPercent float64
	Remaining   float64
	WindowName  string
	WindowSecs  int64
	ResetAt     time.Time
	ResetKey    string
	IsFull      bool
}

func fetchQuota(ctx context.Context, cfg pluginConfig, auth pluginapi.HostAuthFileEntry) (quotaSnapshot, error) {
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
	window, name := selectShortWindow(usage.RateLimit)
	if window == nil || window.UsedPercent == nil {
		return quotaSnapshot{}, fmt.Errorf("wham/usage did not contain a usable 5h quota window")
	}

	used := clamp(*window.UsedPercent, 0, 100)
	resetAt := resolveResetAt(window, time.Now())
	return quotaSnapshot{
		PlanType:    strings.TrimSpace(usage.PlanType),
		UsedPercent: used,
		Remaining:   100 - used,
		WindowName:  name,
		WindowSecs:  windowDurationSeconds(window),
		ResetAt:     resetAt,
		ResetKey:    makeResetKey(resetAt),
		IsFull:      quotaFull(usage.RateLimit, window, cfg.FullUsedPercent),
	}, nil
}

func selectShortWindow(rate whamRateLimit) (*quotaWindow, string) {
	type candidate struct {
		name   string
		window *quotaWindow
	}
	candidates := []candidate{{"primary", rate.PrimaryWindow}, {"secondary", rate.SecondaryWindow}}

	var best *quotaWindow
	bestName := ""
	bestDistance := int64(math.MaxInt64)
	for _, candidate := range candidates {
		if candidate.window == nil || candidate.window.UsedPercent == nil {
			continue
		}
		seconds := windowDurationSeconds(candidate.window)
		if seconds <= 0 {
			continue
		}
		distance := abs64(seconds - targetShortWindowSecs)
		if distance < bestDistance {
			best = candidate.window
			bestName = candidate.name
			bestDistance = distance
		}
	}
	if best != nil {
		return best, formatWindowName(bestName, best)
	}
	if rate.PrimaryWindow != nil && rate.PrimaryWindow.UsedPercent != nil {
		return rate.PrimaryWindow, "primary"
	}
	if rate.SecondaryWindow != nil && rate.SecondaryWindow.UsedPercent != nil {
		return rate.SecondaryWindow, "secondary"
	}
	return nil, ""
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

func makeResetKey(resetAt time.Time) string {
	if resetAt.IsZero() {
		return ""
	}
	return fmt.Sprintf("reset:%d", resetAt.UTC().Truncate(time.Minute).Unix())
}

func formatWindowName(base string, window *quotaWindow) string {
	seconds := windowDurationSeconds(window)
	switch {
	case seconds == targetShortWindowSecs:
		return base + " (5h)"
	case seconds > 0 && seconds%int64(24*time.Hour/time.Second) == 0:
		return fmt.Sprintf("%s (%dd)", base, seconds/int64(24*time.Hour/time.Second))
	case seconds > 0 && seconds%int64(time.Hour/time.Second) == 0:
		return fmt.Sprintf("%s (%dh)", base, seconds/int64(time.Hour/time.Second))
	default:
		return base
	}
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

func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
