package main

import (
	"testing"
	"time"
)

func TestWindowTransitionGuard(t *testing.T) {
	key := "transition-test"
	runtimeState.Lock()
	delete(runtimeState.observations, key)
	delete(runtimeState.accounts, key)
	runtimeState.Unlock()

	if needsWarm := observeQuotaWindows(key, []string{"five-hour", "weekly"}); !needsWarm {
		t.Fatal("first observed full windows must be eligible")
	}

	markWarmSuccess(key, []string{"five-hour", "weekly"}, time.Now())
	if needsWarm := observeQuotaWindows(key, []string{"five-hour", "weekly"}); needsWarm {
		t.Fatal("same full windows must not trigger twice")
	}

	if needsWarm := observeQuotaWindows(key, []string{"weekly"}); needsWarm {
		t.Fatal("still-full weekly window must remain guarded")
	}
	if needsWarm := observeQuotaWindows(key, []string{"five-hour", "weekly"}); !needsWarm {
		t.Fatal("5h transition back to full must become eligible again")
	}
}
