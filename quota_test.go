package main

import (
	"testing"
	"time"
)

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

func TestResetKeyUsesMinutePrecision(t *testing.T) {
	a := time.Date(2026, 9, 26, 12, 34, 1, 0, time.UTC)
	b := time.Date(2026, 9, 26, 12, 34, 59, 0, time.UTC)
	if makeResetKey(a) != makeResetKey(b) {
		t.Fatalf("expected same reset key: %q != %q", makeResetKey(a), makeResetKey(b))
	}
}
