package main

import "testing"

func TestSelectShortWindow(t *testing.T) {
	five := 0.0
	week := 20.0
	rate := whamRateLimit{
		PrimaryWindow:   &quotaWindow{UsedPercent: &week, LimitWindowSec: 7 * 24 * 60 * 60},
		SecondaryWindow: &quotaWindow{UsedPercent: &five, LimitWindowSec: 5 * 60 * 60},
	}
	window, _ := selectShortWindow(rate)
	if window != rate.SecondaryWindow {
		t.Fatalf("expected 5h window, got %#v", window)
	}
}

func TestQuotaFullRespectsAllowedAndLimitReached(t *testing.T) {
	used := 0.0
	allowed := true
	reached := false
	window := &quotaWindow{UsedPercent: &used, LimitWindowSec: 5 * 60 * 60}
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
