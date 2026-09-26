package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type persistedAccountState struct {
	LastWarmAt   time.Time `json:"last_warm_at,omitempty"`
	LastResetKey string    `json:"last_reset_key,omitempty"`
}

type persistedState struct {
	Accounts map[string]persistedAccountState `json:"accounts"`
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
	persisted map[string]persistedAccountState
	accounts  map[string]accountStatus
	running   bool
	nextCheck time.Time
	lastRun   *runSummary
}{
	persisted: map[string]persistedAccountState{},
	accounts:  map[string]accountStatus{},
}

func loadPersistentState(path string) {
	runtimeState.Lock()
	runtimeState.persisted = map[string]persistedAccountState{}
	runtimeState.Unlock()
	if path == "" {
		return
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			pluginLog("read state file failed: %v", err)
		}
		return
	}
	var state persistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		pluginLog("decode state file failed: %v", err)
		return
	}
	if state.Accounts == nil {
		state.Accounts = map[string]persistedAccountState{}
	}
	runtimeState.Lock()
	runtimeState.persisted = state.Accounts
	runtimeState.Unlock()
}

func savePersistentState(path string) {
	if path == "" {
		return
	}
	runtimeState.RLock()
	snapshot := make(map[string]persistedAccountState, len(runtimeState.persisted))
	for key, value := range runtimeState.persisted {
		snapshot[key] = value
	}
	runtimeState.RUnlock()

	raw, err := json.MarshalIndent(persistedState{Accounts: snapshot}, "", "  ")
	if err != nil {
		pluginLog("encode state file failed: %v", err)
		return
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			pluginLog("create state directory failed: %v", err)
			return
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		pluginLog("write state file failed: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		pluginLog("replace state file failed: %v", err)
	}
}

func getPersistent(key string) persistedAccountState {
	runtimeState.RLock()
	defer runtimeState.RUnlock()
	return runtimeState.persisted[key]
}

func markWarmed(key, resetKey, stateFile string, at time.Time) {
	runtimeState.Lock()
	runtimeState.persisted[key] = persistedAccountState{LastWarmAt: at.UTC(), LastResetKey: resetKey}
	status := runtimeState.accounts[key]
	warm := at.UTC()
	status.LastWarmAt = &warm
	status.Status = "warmed"
	status.Error = ""
	runtimeState.accounts[key] = status
	runtimeState.Unlock()
	savePersistentState(stateFile)
}

func updateAccount(key string, status accountStatus) {
	runtimeState.Lock()
	if persisted := runtimeState.persisted[key]; !persisted.LastWarmAt.IsZero() {
		warm := persisted.LastWarmAt
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
