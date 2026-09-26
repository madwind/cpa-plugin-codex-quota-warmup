package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func managementRegistration() pluginapi.ManagementRegistrationResponse {
	return pluginapi.ManagementRegistrationResponse{
		Resources: []pluginapi.ResourceRoute{
			{
				Path:        "/status",
				Menu:        "Codex Quota Warmup",
				Description: "Read-only background warm-up status.",
			},
		},
	}
}

func handleManagementRPC(raw []byte) ([]byte, error) {
	var req pluginapi.ManagementRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode management request: %w", err)
	}

	path := strings.TrimSpace(req.Path)
	if req.Method == http.MethodGet &&
		strings.Contains(path, "/v0/resource/plugins/") &&
		strings.HasSuffix(path, "/status") {
		resp, err := resourceHandler(context.Background(), req)
		if err != nil {
			return nil, err
		}
		return okEnvelope(resp)
	}

	return okEnvelope(pluginapi.ManagementResponse{
		StatusCode: http.StatusNotFound,
		Headers:    http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}},
		Body:       []byte("not found"),
	})
}

func resourceHandler(_ context.Context, _ pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	snapshot := runtimeStatusSnapshot(loadedConfig())
	return pluginapi.ManagementResponse{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Type":  []string{"text/html; charset=utf-8"},
			"Cache-Control": []string{"no-store"},
		},
		Body: []byte(renderStatusPage(snapshot)),
	}, nil
}

func renderStatusPage(snapshot runtimeSnapshot) string {
	var out strings.Builder
	out.WriteString(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Codex Quota Warmup</title><style>
:root{--bg-secondary:#faf9f5;--bg-primary:#f0eee8;--bg-tertiary:#e9e6df;--bg-hover:var(--bg-tertiary);--text-primary:#2d2a26;--text-secondary:#6d6760;--text-tertiary:#a29c95;--border-color:#e3e1db;--border-primary:#d5d2cb;--primary-color:#8b8680;--success-color:#10b981;--warning-color:#c65746;--shadow:0 1px 2px rgb(0 0 0/.08)}
[data-theme="white"]{--bg-secondary:#fff;--bg-primary:#fff;--bg-tertiary:#f6f6f6;--text-primary:#2d2a26;--text-secondary:#6d6760;--text-tertiary:#a29c95;--border-color:#e5e5e5;--border-primary:#d9d9d9}
[data-theme="dark"]{--bg-secondary:#151412;--bg-primary:#1d1b18;--bg-tertiary:#262320;--bg-hover:#2e2a26;--text-primary:#f6f4f1;--text-secondary:#c9c3bb;--text-tertiary:#9c958d;--border-color:#3a3530;--border-primary:#4a453f;--primary-color:#8b8680;--success-color:#10b981;--warning-color:#c65746;--shadow:0 1px 3px rgb(0 0 0/.3)}
*{box-sizing:border-box}html,body{margin:0;min-height:100%;background:var(--bg-secondary);color:var(--text-primary)}body{font-family:Inter,ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;font-size:14px;line-height:1.5}
.page{width:min(1180px,100%);margin:0 auto;padding:30px clamp(18px,3vw,42px) 48px}.header{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-bottom:22px}.title{margin:0;font-size:26px;line-height:1.2;font-weight:700;letter-spacing:-.02em}.subtitle{margin:7px 0 0;color:var(--text-secondary)}.badge{display:inline-flex;align-items:center;gap:7px;padding:5px 9px;border:1px solid var(--border-color);border-radius:999px;background:var(--bg-primary);font-size:12px}.dot{width:8px;height:8px;border-radius:50%;background:var(--text-tertiary)}.dot.ok{background:var(--success-color)}.dot.warn{background:var(--warning-color)}
.grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px;margin-bottom:18px}.card{background:var(--bg-primary);border:1px solid var(--border-color);border-radius:12px;box-shadow:var(--shadow)}.metric{padding:15px 16px}.metric-label{font-size:12px;color:var(--text-secondary);margin-bottom:5px}.metric-value{font-size:17px;font-weight:650;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.metric-sub{margin-top:4px;font-size:12px;color:var(--text-secondary)}
.section{overflow:hidden}.section-head{display:flex;align-items:center;justify-content:space-between;padding:15px 16px;border-bottom:1px solid var(--border-color)}.section-title{font-size:15px;font-weight:650}.table-wrap{overflow:auto}table{width:100%;border-collapse:collapse;min-width:900px}th,td{text-align:left;padding:12px 16px;border-bottom:1px solid var(--border-color);vertical-align:top}th{font-size:12px;color:var(--text-secondary);font-weight:600;background:color-mix(in srgb,var(--bg-tertiary) 55%,transparent)}tbody tr:last-child td{border-bottom:0}.account{font-weight:600}.muted{font-size:12px;color:var(--text-secondary)}.windows{display:flex;gap:7px;flex-wrap:wrap}.window{min-width:112px;padding:7px 9px;border:1px solid var(--border-color);border-radius:8px;background:var(--bg-secondary)}.window-name{font-size:11px;color:var(--text-secondary)}.window-value{font-weight:650;margin-top:1px}.window-reset{font-size:11px;color:var(--text-tertiary);margin-top:2px}.status{display:inline-flex;align-items:center;gap:6px}.empty{padding:30px 16px;text-align:center;color:var(--text-secondary)}
.note{margin-top:14px;color:var(--text-tertiary);font-size:12px}.summary-line{display:flex;gap:16px;flex-wrap:wrap;color:var(--text-secondary);font-size:12px}
@media(max-width:840px){.header{flex-direction:column}.grid{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:520px){.grid{grid-template-columns:1fr}.page{padding-left:14px;padding-right:14px}}
</style></head><body><div class="page">`)

	workerLabel := "Stopped"
	workerClass := "warn"
	if snapshot.WorkerActive {
		workerLabel = "Online"
		workerClass = "ok"
	}
	if snapshot.Running {
		workerLabel = "Checking"
	}

	out.WriteString(`<div class="header"><div><h1 class="title">Codex Quota Warmup</h1><p class="subtitle">Background quota-window warm-up status. This page is read-only and requires no Management Key.</p></div><span class="badge"><span class="dot ` + workerClass + `"></span>` + html.EscapeString(workerLabel) + `</span></div>`)

	writeMetric := func(label, value, sub string) {
		out.WriteString(`<div class="card metric"><div class="metric-label">` + html.EscapeString(label) + `</div><div class="metric-value">` + html.EscapeString(value) + `</div>`)
		if sub != "" {
			out.WriteString(`<div class="metric-sub">` + html.EscapeString(sub) + `</div>`)
		}
		out.WriteString(`</div>`)
	}

	out.WriteString(`<div class="grid">`)
	writeMetric("Last check", formatDashboardTime(lastCheckTime(snapshot)), runSummaryText(snapshot.LastRun))
	writeMetric("Next check", formatDashboardTimePtr(snapshot.NextCheck), "Interval "+snapshot.Interval)
	writeMetric("Last warm-up", formatDashboardTimePtr(snapshot.LastWarmAt), strconv.FormatUint(snapshot.TotalWarmups, 10)+" successful warm-ups")
	writeMetric("Model", snapshot.Model, "Telegram "+onOff(snapshot.TelegramConfigured))
	out.WriteString(`</div>`)

	out.WriteString(`<section class="card section"><div class="section-head"><div class="section-title">Observed accounts</div><div class="summary-line"><span>Runs ` + strconv.FormatUint(snapshot.TotalRuns, 10) + `</span><span>Checks ` + strconv.FormatUint(snapshot.TotalChecks, 10) + `</span><span>Failures ` + strconv.FormatUint(snapshot.TotalFailures, 10) + `</span></div></div>`)
	out.WriteString(`<div class="table-wrap"><table><thead><tr><th>Account</th><th>Plan</th><th>Quota windows</th><th>Status</th><th>Last checked</th><th>Last warm-up</th></tr></thead><tbody>`)
	if len(snapshot.Accounts) == 0 {
		out.WriteString(`<tr><td colspan="6" class="empty">Waiting for the first background quota check.</td></tr>`)
	} else {
		for i, account := range snapshot.Accounts {
			out.WriteString(`<tr><td><div class="account">Codex #` + strconv.Itoa(i+1) + `</div><div class="muted">Identifiers hidden</div></td>`)
			plan := account.PlanType
			if plan == "" {
				plan = "-"
			}
			out.WriteString(`<td>` + html.EscapeString(plan) + `</td><td><div class="windows">`)
			if len(account.Windows) == 0 {
				out.WriteString(`<span class="muted">No quota data</span>`)
			} else {
				for _, window := range account.Windows {
					out.WriteString(`<div class="window"><div class="window-name">` + html.EscapeString(window.Label) + `</div><div class="window-value">` + formatPercent(window.Remaining) + ` remaining</div><div class="window-reset">Reset ` + html.EscapeString(formatDashboardTimePtr(window.ResetAt)) + `</div></div>`)
				}
			}
			out.WriteString(`</div></td>`)
			statusLabel, statusClass := dashboardStatus(account.Status)
			out.WriteString(`<td><span class="status"><span class="dot ` + statusClass + `"></span>` + html.EscapeString(statusLabel) + `</span></td>`)
			out.WriteString(`<td>` + html.EscapeString(formatDashboardTimePtr(account.LastCheckedAt)) + `</td><td>` + html.EscapeString(formatDashboardTimePtr(account.LastWarmAt)) + `</td></tr>`)
		}
	}
	out.WriteString(`</tbody></table></div></section><div class="note">Account names, e-mail addresses and auth IDs are intentionally hidden because CPA resource pages are browser-navigable without Management API authentication. Refreshes automatically every 30 seconds.</div></div>`)
	out.WriteString(`<script>
function syncTheme(){let t='';try{if(parent!==window&&parent.location.origin===location.origin)t=parent.document.documentElement.getAttribute('data-theme')||''}catch(e){}if(!t&&matchMedia&&matchMedia('(prefers-color-scheme: dark)').matches)t='dark';if(t==='dark'||t==='white')document.documentElement.setAttribute('data-theme',t);else document.documentElement.removeAttribute('data-theme')}
syncTheme();try{if(parent!==window&&parent.location.origin===location.origin)new MutationObserver(syncTheme).observe(parent.document.documentElement,{attributes:true,attributeFilter:['data-theme']})}catch(e){}setTimeout(()=>location.reload(),30000);
</script></body></html>`)
	return out.String()
}

func lastCheckTime(snapshot runtimeSnapshot) *time.Time {
	if snapshot.LastRun == nil || snapshot.LastRun.FinishedAt.IsZero() {
		return nil
	}
	value := snapshot.LastRun.FinishedAt
	return &value
}

func runSummaryText(summary *runSummary) string {
	if summary == nil {
		return "No completed check yet"
	}
	return fmt.Sprintf("Checked %d · Warmed %d · Failed %d", summary.Checked, summary.Warmed, summary.Failed)
}

func formatDashboardTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04:05")
}

func formatDashboardTimePtr(value *time.Time) string {
	return formatDashboardTime(value)
}

func formatPercent(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64) + "%"
}

func onOff(value bool) string {
	if value {
		return "On"
	}
	return "Off"
}

func dashboardStatus(status string) (string, string) {
	switch status {
	case "warmed":
		return "Warmed", "ok"
	case "waiting":
		return "Waiting", "ok"
	case "full_already_triggered":
		return "Already warmed", "ok"
	case "warming":
		return "Warming", "ok"
	case "disabled":
		return "Disabled", ""
	case "quota_error", "warm_error", "error":
		return "Error", "warn"
	default:
		if status == "" || status == "idle" {
			return "Idle", ""
		}
		return status, ""
	}
}
