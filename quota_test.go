package main

import (
	"testing"
	"time"
)

func TestTrackedQuotaWindowsClassifiesFiveHourAndWeekly(t *testing.T) {
	five := 0.0
	week := 20.0
	rate := whamRateLimit{
		PrimaryWindow:   &quotaWindow{UsedPercent: &week, LimitWindowSec: weeklyWindowSecs},
		SecondaryWindow: &quotaWindow{UsedPercent: &five, LimitWindowSec: fiveHourWindowSecs},
	}
	windows := trackedQuotaWindows(rate, 0, time.Unix(0, 0))
	if len(windows) != 2 {
		t.Fatalf("expected two tracked windows, got %d", len(windows))
	}
	found := map[string]quotaWindowSnapshot{}
	for _, window := range windows {
		found[window.ID] = window
	}
	if !found["five-hour"].IsFull {
		t.Fatal("expected full 5h window")
	}
	if found["weekly"].Remaining != 80 {
		t.Fatalf("expected 80%% weekly remaining, got %.2f", found["weekly"].Remaining)
	}
}

func TestTrackedQuotaWindowsClassifiesMonthly(t *testing.T) {
	used := 12.5
	rate := whamRateLimit{
		SecondaryWindow: &quotaWindow{UsedPercent: &used, LimitWindowSec: 30 * 24 * 60 * 60},
	}
	windows := trackedQuotaWindows(rate, 0, time.Unix(0, 0))
	if len(windows) != 1 || windows[0].ID != "monthly" {
		t.Fatalf("expected monthly window, got %#v", windows)
	}
}

func TestQuotaFullRespectsAllowedAndLimitReached(t *testing.T) {
	used := 0.0
	allowed := true
	reached := false
	window := &quotaWindow{UsedPercent: &used, LimitWindowSec: fiveHourWindowSecs}
	if !quotaFull(whamRateLimit{Allowed: &allowed, LimitReached: &reached}, window, 0) {
		t.Fatal("expected full quota")
	}
	allowed = false
	if quotaFull(whamRateLimit{Allowed: &allowed, LimitReached: &reached}, window, 0) {
		t.Fatal("disallowed quota must not warm")
	}
	allowed = true
	reached = true
	if quotaFull(whamRateLimit{Allowed: &allowed, LimitReached: &reached}, window, 0) {
		t.Fatal("limit-reached quota must not warm")
	}
}
