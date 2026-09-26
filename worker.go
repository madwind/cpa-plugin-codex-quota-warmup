package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

var workerControl = struct {
	sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}{}

func restartWorker(cfg pluginConfig) {
	stopWorker()
	loadPersistentState(cfg.StateFile)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	workerControl.Lock()
	workerControl.cancel = cancel
	workerControl.done = done
	workerControl.Unlock()

	go func() {
		defer close(done)
		runWorker(ctx, cfg)
	}()
}

func stopWorker() {
	workerControl.Lock()
	cancel := workerControl.cancel
	done := workerControl.done
	workerControl.cancel = nil
	workerControl.done = nil
	workerControl.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func runWorker(ctx context.Context, cfg pluginConfig) {
	pluginLog("started interval=%s model=%s", cfg.Interval, cfg.Model)

	if cfg.initialDelayDuration > 0 {
		setNextCheck(time.Now().Add(cfg.initialDelayDuration))
		timer := time.NewTimer(cfg.initialDelayDuration)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}

	runCheck(ctx, cfg, "scheduled", false, "")
	ticker := time.NewTicker(cfg.intervalDuration)
	defer ticker.Stop()
	for {
		setNextCheck(time.Now().Add(cfg.intervalDuration))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCheck(ctx, cfg, "scheduled", false, "")
		}
	}
}

func setNextCheck(at time.Time) {
	runtimeState.Lock()
	runtimeState.nextCheck = at.UTC()
	runtimeState.Unlock()
}

func claimRun() bool {
	runtimeState.Lock()
	defer runtimeState.Unlock()
	if runtimeState.running {
		return false
	}
	runtimeState.running = true
	return true
}

func finishRun(summary runSummary) {
	runtimeState.Lock()
	runtimeState.running = false
	runtimeState.lastRun = &summary
	runtimeState.Unlock()
}

func runCheck(ctx context.Context, cfg pluginConfig, mode string, forceWarm bool, onlyAuthID string) runSummary {
	summary := runSummary{StartedAt: time.Now().UTC(), Mode: mode}
	if !claimRun() {
		summary.FinishedAt = time.Now().UTC()
		return summary
	}
	return runCheckClaimed(ctx, cfg, mode, forceWarm, onlyAuthID)
}

func runCheckClaimed(ctx context.Context, cfg pluginConfig, mode string, forceWarm bool, onlyAuthID string) (summary runSummary) {
	summary = runSummary{StartedAt: time.Now().UTC(), Mode: mode}
	defer func() {
		summary.FinishedAt = time.Now().UTC()
		finishRun(summary)
	}()

	auths, err := listCodexAuths(ctx)
	if err != nil {
		summary.Failed++
		pluginLog("list Codex auths failed: %v", err)
		if cfg.NotifyPollFailures {
			_ = sendTelegram(ctx, cfg, fmt.Sprintf("⚠️ CPA Codex quota check failed\n%v", err))
		}
		return summary
	}

	for _, auth := range auths {
		if ctx.Err() != nil {
			break
		}
		if onlyAuthID != "" && auth.ID != onlyAuthID {
			continue
		}
		key := accountKey(auth)
		base := accountStatus{
			AuthID:      auth.ID,
			AuthIndex:   auth.AuthIndex,
			Name:        authDisplay(auth),
			Email:       strings.TrimSpace(auth.Email),
			Disabled:    auth.Disabled,
			Unavailable: auth.Unavailable,
			Status:      "idle",
		}
		if auth.Disabled {
			base.Status = "disabled"
			updateAccount(key, base)
			summary.Skipped++
			continue
		}
		if strings.TrimSpace(auth.ID) == "" || strings.TrimSpace(auth.AuthIndex) == "" {
			base.Status = "error"
			base.Error = "missing auth id or auth_index"
			updateAccount(key, base)
			summary.Failed++
			continue
		}

		summary.Checked++
		checkedAt := time.Now().UTC()
		quota, err := fetchQuota(ctx, cfg, auth)
		if err != nil {
			base.Status = "quota_error"
			base.Error = err.Error()
			base.LastCheckedAt = &checkedAt
			updateAccount(key, base)
			summary.Failed++
			pluginLog("quota check failed account=%s: %v", authDisplay(auth), err)
			if cfg.NotifyPollFailures {
				_ = sendTelegram(ctx, cfg, fmt.Sprintf("⚠️ CPA Codex quota check failed\nAccount: %s\nError: %v", authDisplay(auth), err))
			}
			continue
		}

		used := quota.UsedPercent
		remaining := quota.Remaining
		base.PlanType = quota.PlanType
		base.UsedPercent = &used
		base.Remaining = &remaining
		base.Window = quota.WindowName
		base.LastCheckedAt = &checkedAt
		if !quota.ResetAt.IsZero() {
			reset := quota.ResetAt
			base.ResetAt = &reset
		}

		persisted := getPersistent(key)
		shouldWarm := forceWarm || quota.IsFull
		if !forceWarm && shouldWarm {
			if quota.ResetKey != "" && persisted.LastResetKey == quota.ResetKey {
				shouldWarm = false
				base.Status = "already_warmed_cycle"
			} else if cfg.minWarmDuration > 0 && !persisted.LastWarmAt.IsZero() && time.Since(persisted.LastWarmAt) < cfg.minWarmDuration {
				shouldWarm = false
				base.Status = "warm_guard"
			}
		}
		if !shouldWarm {
			if base.Status == "idle" {
				base.Status = "waiting"
			}
			updateAccount(key, base)
			summary.Skipped++
			continue
		}

		base.Status = "warming"
		updateAccount(key, base)
		warmCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		err = executeWarmup(warmCtx, cfg, auth)
		cancel()
		if err != nil {
			base.Status = "warm_error"
			base.Error = err.Error()
			updateAccount(key, base)
			summary.Failed++
			pluginLog("warm-up failed account=%s: %v", authDisplay(auth), err)
			if cfg.notifyFailureValue && telegramConfigured(cfg) {
				_ = sendTelegram(ctx, cfg, fmt.Sprintf(
					"❌ CPA Codex warm-up failed\nAccount: %s\n5h remaining: %.2f%%\nError: %v",
					authDisplay(auth), quota.Remaining, err,
				))
			}
			continue
		}

		warmedAt := time.Now().UTC()
		markWarmed(key, quota.ResetKey, cfg.StateFile, warmedAt)
		base.Status = "warmed"
		base.Error = ""
		base.LastWarmAt = &warmedAt
		updateAccount(key, base)
		summary.Warmed++
		pluginLog("warm-up success account=%s remaining_before=%.2f%%", authDisplay(auth), quota.Remaining)
		if cfg.notifySuccessValue && telegramConfigured(cfg) {
			message := fmt.Sprintf(
				"✅ CPA Codex warm-up\nAccount: %s\n5h remaining before ping: %.2f%%\nModel: %s\nAction: ping sent",
				authDisplay(auth), quota.Remaining, cfg.Model,
			)
			if quota.PlanType != "" {
				message += "\nPlan: " + quota.PlanType
			}
			if !quota.ResetAt.IsZero() {
				message += "\nReset: " + quota.ResetAt.Format(time.RFC3339)
			}
			_ = sendTelegram(ctx, cfg, message)
		}
	}
	return summary
}

func startAsyncRun(cfg pluginConfig, mode string, forceWarm bool, onlyAuthID string) bool {
	if !claimRun() {
		return false
	}
	go runCheckClaimed(context.Background(), cfg, mode, forceWarm, onlyAuthID)
	return true
}

func accountKey(auth pluginapi.HostAuthFileEntry) string {
	if strings.TrimSpace(auth.ID) != "" {
		return auth.ID
	}
	return auth.AuthIndex
}
