package main

import (
	"testing"
	"time"
)

func TestFullTransitionGuard(t *testing.T) {
	key := "transition-test"
	runtimeState.Lock()
	delete(runtimeState.observations, key)
	delete(runtimeState.accounts, key)
	runtimeState.Unlock()

	if already := observeQuota(key, true); already {
		t.Fatal("first observed full state must be eligible")
	}

	markWarmSuccess(key, true, time.Now())
	if already := observeQuota(key, true); !already {
		t.Fatal("same full state must not trigger twice")
	}

	if already := observeQuota(key, false); already {
		t.Fatal("not-full observation must clear full-state guard")
	}
	if already := observeQuota(key, true); already {
		t.Fatal("a later transition back to full must be eligible again")
	}
}
