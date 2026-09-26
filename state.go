package main

import (
	"sort"
	"sync"
	"time"
)

type accountObservation struct {
	FullTriggered bool
	LastWarmAt    time.Time
}

type accountStatus struct {
	AuthID        string     `json:"auth_id"`
	AuthIndex     string     `json:"auth_index,omitempty"`
	Name          string     `json:"name"`
	Email         string     `json:"email,omitempty"`
	Disabled      bool       `json:"disabled"`
	Unavailable   bool       `json:"unavailable"`
	PlanType      string     `json:"plan_type,omitempty"`
	UsedPercent   *float64   `json:"used_percent,omitempty"`
	Remaining     *float64   `json:"remaining_percent,omitempty"`
	Window        string     `json:"window,omitempty"`
	ResetAt       *time.Time `json:"reset_at,omitempty"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	LastWarmAt    *time.Time `json:"last_warm_at,omitempty"`
	Status        string     `json:"status"`
	Error         string     `json:"error,omitempty"`
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
	Accounts           []accountStatus `json:"accounts"`
}

var runtimeState = struct {
	sync.RWMutex
	observations map[string]accountObservation
	accounts     map[string]accountStatus
	running      bool
	nextCheck    time.Time
	lastRun      *runSummary
}{
	observations: map[string]accountObservation{},
	accounts:     map[string]accountStatus{},
}

// observeQuota records only the transition guard. The quota value itself is never
// cached as the source of truth; every worker cycle obtains a fresh value through CPA.
// The return value reports whether the current full state was already triggered.
func observeQuota(key string, full bool) bool {
	runtimeState.Lock()
	defer runtimeState.Unlock()

	obs := runtimeState.observations[key]
	if !full {
		obs.FullTriggered = false
		runtimeState.observations[key] = obs
		return false
	}
	return obs.FullTriggered
}

func markWarmSuccess(key string, quotaWasFull bool, at time.Time) {
	runtimeState.Lock()
	obs := runtimeState.observations[key]
	if quotaWasFull {
		obs.FullTriggered = true
	}
	obs.LastWarmAt = at.UTC()
	runtimeState.observations[key] = obs

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
	return runtimeSnapshot{
		Version:            pluginVersion,
		Running:            runtimeState.running,
		Interval:           cfg.Interval,
		Model:              cfg.Model,
		FullUsedPercent:    cfg.FullUsedPercent,
		TelegramConfigured: telegramConfigured(cfg),
		NextCheck:          next,
		LastRun:            last,
		Accounts:           accounts,
	}
}
