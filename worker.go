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

	runCheck(ctx, cfg)
	ticker := time.NewTicker(cfg.intervalDuration)
	defer ticker.Stop()
	for {
		setNextCheck(time.Now().Add(cfg.intervalDuration))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCheck(ctx, cfg)
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
	runtimeState.totalRuns++
	runtimeState.totalChecks += uint64(summary.Checked)
	runtimeState.totalWarmups += uint64(summary.Warmed)
	runtimeState.totalFailures += uint64(summary.Failed)
	runtimeState.Unlock()
}

func runCheck(ctx context.Context, cfg pluginConfig) runSummary {
	summary := runSummary{StartedAt: time.Now().UTC(), Mode: "scheduled"}
	if !claimRun() {
		summary.FinishedAt = time.Now().UTC()
		return summary
	}
	return runCheckClaimed(ctx, cfg)
}

func runCheckClaimed(ctx context.Context, cfg pluginConfig) (summary runSummary) {
	summary = runSummary{StartedAt: time.Now().UTC(), Mode: "scheduled"}
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

		quota, err := fetchQuotaViaCPA(ctx, cfg, auth)
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

		base.PlanType = quota.PlanType
		base.LastCheckedAt = &checkedAt
		base.Windows = make([]quotaWindowStatus, 0, len(quota.Windows))
		for _, window := range quota.Windows {
			item := quotaWindowStatus{
				ID:            window.ID,
				Label:         window.Label,
				UsedPercent:   window.UsedPercent,
				Remaining:     window.Remaining,
				WindowSeconds: window.WindowSecs,
			}
			if !window.ResetAt.IsZero() {
				reset := window.ResetAt
				item.ResetAt = &reset
			}
			base.Windows = append(base.Windows, item)
		}

		fullIDs := quota.fullWindowIDs()
		needsWarm := observeQuotaWindows(key, fullIDs)
		if !needsWarm {
			if len(fullIDs) > 0 {
				base.Status = "full_already_triggered"
			} else {
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
			pluginLog("warm-up failed account=%s windows=%s: %v", authDisplay(auth), strings.Join(quota.fullWindowLabels(), ","), err)
			if cfg.notifyFailureValue && telegramConfigured(cfg) {
				_ = sendTelegram(ctx, cfg, fmt.Sprintf(
					"❌ CPA Codex warm-up failed\nAccount: %s\nWindows: %s\nError: %v",
					authDisplay(auth), strings.Join(quota.fullWindowLabels(), ", "), err,
				))
			}
			continue
		}

		warmedAt := time.Now().UTC()
		markWarmSuccess(key, fullIDs, warmedAt)
		base.Status = "warmed"
		base.Error = ""
		base.LastWarmAt = &warmedAt
		updateAccount(key, base)
		summary.Warmed++
		pluginLog("warm-up success account=%s windows=%s", authDisplay(auth), strings.Join(quota.fullWindowLabels(), ","))

		if cfg.notifySuccessValue && telegramConfigured(cfg) {
			message := fmt.Sprintf(
				"✅ CPA Codex warm-up\nAccount: %s\nWindows: %s\nModel: %s\nAction: ping sent",
				authDisplay(auth), strings.Join(quota.fullWindowLabels(), ", "), cfg.Model,
			)
			if quota.PlanType != "" {
				message += "\nPlan: " + quota.PlanType
			}
			_ = sendTelegram(ctx, cfg, message)
		}
	}
	return summary
}

func accountKey(auth pluginapi.HostAuthFileEntry) string {
	if strings.TrimSpace(auth.ID) != "" {
		return auth.ID
	}
	return auth.AuthIndex
}
