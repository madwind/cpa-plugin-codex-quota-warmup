package main

import (
	"sort"
	"sync"
	"time"
)

type accountObservation struct {
	TriggeredWindows map[string]bool
	LastWarmAt       time.Time
}

type quotaWindowStatus struct {
	ID            string     `json:"id"`
	Label         string     `json:"label"`
	UsedPercent   float64    `json:"used_percent"`
	Remaining     float64    `json:"remaining_percent"`
	WindowSeconds int64      `json:"window_seconds,omitempty"`
	ResetAt       *time.Time `json:"reset_at,omitempty"`
}

type accountStatus struct {
	AuthID        string              `json:"auth_id"`
	AuthIndex     string              `json:"auth_index,omitempty"`
	Name          string              `json:"name"`
	Email         string              `json:"email,omitempty"`
	Disabled      bool                `json:"disabled"`
	Unavailable   bool                `json:"unavailable"`
	PlanType      string              `json:"plan_type,omitempty"`
	Windows       []quotaWindowStatus `json:"windows,omitempty"`
	LastCheckedAt *time.Time          `json:"last_checked_at,omitempty"`
	LastWarmAt    *time.Time          `json:"last_warm_at,omitempty"`
	Status        string              `json:"status"`
	Error         string              `json:"error,omitempty"`
}

type runSummary struct {
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Mode       string    `json:"mode"`
	Checked    int       `json:"checked"`
	Warmed     int       `json:"warmed"`
	Skipped    int       `json:"skipped"`
	Failed     int       `json:"failed"`
}

type runtimeSnapshot struct {
	Version            string          `json:"version"`
	Running            bool            `json:"running"`
	Interval           string          `json:"interval"`
	Model              string          `json:"model"`
	FullUsedPercent    float64         `json:"full_used_percent"`
	TelegramConfigured bool            `json:"telegram_configured"`
	NextCheck          *time.Time      `json:"next_check,omitempty"`
	LastRun            *runSummary     `json:"last_run,omitempty"`
	LastWarmAt         *time.Time      `json:"last_warm_at,omitempty"`
	TotalRuns          uint64          `json:"total_runs"`
	TotalChecks        uint64          `json:"total_checks"`
	TotalWarmups       uint64          `json:"total_warmups"`
	TotalFailures      uint64          `json:"total_failures"`
	Accounts           []accountStatus `json:"accounts"`
}

var runtimeState = struct {
	sync.RWMutex
	observations  map[string]accountObservation
	accounts      map[string]accountStatus
	running       bool
	nextCheck     time.Time
	lastRun       *runSummary
	lastWarmAt    time.Time
	totalRuns     uint64
	totalChecks   uint64
	totalWarmups  uint64
	totalFailures uint64
}{
	observations: map[string]accountObservation{},
	accounts:     map[string]accountStatus{},
}

// observeQuotaWindows updates the per-window transition guard.
// It returns true when at least one currently-full quota window has not yet
// triggered a warm-up. Windows that are no longer full have their guard cleared.
func observeQuotaWindows(key string, fullWindowIDs []string) bool {
	runtimeState.Lock()
	defer runtimeState.Unlock()

	obs := runtimeState.observations[key]
	if obs.TriggeredWindows == nil {
		obs.TriggeredWindows = map[string]bool{}
	}

	current := make(map[string]bool, len(fullWindowIDs))
	for _, id := range fullWindowIDs {
		if id != "" {
			current[id] = true
		}
	}
	for id := range obs.TriggeredWindows {
		if !current[id] {
			delete(obs.TriggeredWindows, id)
		}
	}

	needsWarm := false
	for id := range current {
		if !obs.TriggeredWindows[id] {
			needsWarm = true
		}
	}

	runtimeState.observations[key] = obs
	return needsWarm
}

func markWarmSuccess(key string, fullWindowIDs []string, at time.Time) {
	runtimeState.Lock()
	obs := runtimeState.observations[key]
	if obs.TriggeredWindows == nil {
		obs.TriggeredWindows = map[string]bool{}
	}
	for _, id := range fullWindowIDs {
		if id != "" {
			obs.TriggeredWindows[id] = true
		}
	}
	obs.LastWarmAt = at.UTC()
	runtimeState.observations[key] = obs
	runtimeState.lastWarmAt = at.UTC()

	status := runtimeState.accounts[key]
	warm := at.UTC()
	status.LastWarmAt = &warm
	status.Status = "warmed"
	status.Error = ""
	runtimeState.accounts[key] = status
	runtimeState.Unlock()
}

func updateAccount(key string, status accountStatus) {
	runtimeState.Lock()
	if obs := runtimeState.observations[key]; !obs.LastWarmAt.IsZero() {
		warm := obs.LastWarmAt
		status.LastWarmAt = &warm
	}
	runtimeState.accounts[key] = status
	runtimeState.Unlock()
}

func runtimeStatusSnapshot(cfg pluginConfig) runtimeSnapshot {
	runtimeState.RLock()
	defer runtimeState.RUnlock()

	accounts := make([]accountStatus, 0, len(runtimeState.accounts))
	for _, status := range runtimeState.accounts {
		accounts = append(accounts, status)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Name < accounts[j].Name })

	var next *time.Time
	if !runtimeState.nextCheck.IsZero() {
		value := runtimeState.nextCheck
		next = &value
	}
	var last *runSummary
	if runtimeState.lastRun != nil {
		value := *runtimeState.lastRun
		last = &value
	}
	var lastWarm *time.Time
	if !runtimeState.lastWarmAt.IsZero() {
		value := runtimeState.lastWarmAt
		lastWarm = &value
	}
	return runtimeSnapshot{
		Version:            pluginVersion,
		Running:            runtimeState.running,
		Interval:           cfg.Interval,
		Model:              cfg.Model,
		FullUsedPercent:    cfg.FullUsedPercent,
		TelegramConfigured: telegramConfigured(cfg),
		NextCheck:          next,
		LastRun:            last,
		LastWarmAt:         lastWarm,
		TotalRuns:          runtimeState.totalRuns,
		TotalChecks:        runtimeState.totalChecks,
		TotalWarmups:       runtimeState.totalWarmups,
		TotalFailures:      runtimeState.totalFailures,
		Accounts:           accounts,
	}
}
