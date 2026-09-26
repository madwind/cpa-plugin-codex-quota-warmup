package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type managementHandlerFunc func(context.Context, pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error)

func (fn managementHandlerFunc) HandleManagement(ctx context.Context, req pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	return fn(ctx, req)
}

func managementRegistration() pluginapi.ManagementRegistrationResponse {
	return pluginapi.ManagementRegistrationResponse{
		Routes: []pluginapi.ManagementRoute{
			{Method: http.MethodGet, Path: "/plugins/" + pluginID + "/status", Description: "Return quota warm-up status.", Handler: managementHandlerFunc(statusHandler)},
			{Method: http.MethodPost, Path: "/plugins/" + pluginID + "/check", Description: "Run an immediate quota check.", Handler: managementHandlerFunc(checkHandler)},
			{Method: http.MethodPost, Path: "/plugins/" + pluginID + "/ping", Description: "Force a warm-up request for one Codex auth.", Handler: managementHandlerFunc(pingHandler)},
			{Method: http.MethodPost, Path: "/plugins/" + pluginID + "/telegram/test", Description: "Send a Telegram test message.", Handler: managementHandlerFunc(telegramTestHandler)},
		},
		Resources: []pluginapi.ResourceRoute{
			{Path: "/status", Menu: "Codex Quota Warmup", Description: "Codex quota status, manual check, warm-up and Telegram diagnostics.", Handler: managementHandlerFunc(resourceHandler)},
		},
	}
}

func handleManagementRPC(raw []byte) ([]byte, error) {
	var req pluginapi.ManagementRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode management request: %w", err)
	}

	var handler managementHandlerFunc
	path := strings.TrimSpace(req.Path)
	switch {
	case req.Method == http.MethodGet && strings.Contains(path, "/v0/resource/plugins/") && strings.HasSuffix(path, "/status"):
		handler = resourceHandler
	case req.Method == http.MethodGet && strings.HasSuffix(path, "/plugins/"+pluginID+"/status"):
		handler = statusHandler
	case req.Method == http.MethodPost && strings.HasSuffix(path, "/plugins/"+pluginID+"/check"):
		handler = checkHandler
	case req.Method == http.MethodPost && strings.HasSuffix(path, "/plugins/"+pluginID+"/ping"):
		handler = pingHandler
	case req.Method == http.MethodPost && strings.HasSuffix(path, "/plugins/"+pluginID+"/telegram/test"):
		handler = telegramTestHandler
	default:
		return okEnvelope(jsonResponse(http.StatusNotFound, map[string]any{"error": "not_found"}))
	}

	resp, err := handler(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return okEnvelope(resp)
}

func statusHandler(_ context.Context, _ pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	return jsonResponse(http.StatusOK, runtimeStatusSnapshot(loadedConfig())), nil
}

func checkHandler(_ context.Context, _ pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	cfg := loadedConfig()
	if !startAsyncRun(cfg, "manual-check", false, "") {
		return jsonResponse(http.StatusConflict, map[string]any{"accepted": false, "message": "a quota check is already running"}), nil
	}
	return jsonResponse(http.StatusAccepted, map[string]any{"accepted": true}), nil
}

func pingHandler(_ context.Context, req pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	var payload struct {
		AuthID string `json:"auth_id"`
	}
	if err := json.Unmarshal(req.Body, &payload); err != nil || strings.TrimSpace(payload.AuthID) == "" {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": "auth_id is required"}), nil
	}
	cfg := loadedConfig()
	if !startAsyncRun(cfg, "manual-ping", true, strings.TrimSpace(payload.AuthID)) {
		return jsonResponse(http.StatusConflict, map[string]any{"accepted": false, "message": "another run is already active"}), nil
	}
	return jsonResponse(http.StatusAccepted, map[string]any{"accepted": true, "auth_id": strings.TrimSpace(payload.AuthID)}), nil
}

func telegramTestHandler(_ context.Context, _ pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	cfg := loadedConfig()
	if !telegramConfigured(cfg) {
		return jsonResponse(http.StatusBadRequest, map[string]any{"ok": false, "error": "Telegram is not configured"}), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := sendTelegram(ctx, cfg, "✅ CPA Codex Quota Warmup Telegram test"); err != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()}), nil
	}
	return jsonResponse(http.StatusOK, map[string]any{"ok": true}), nil
}

func resourceHandler(_ context.Context, _ pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	return pluginapi.ManagementResponse{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Type":  []string{"text/html; charset=utf-8"},
			"Cache-Control": []string{"no-store"},
		},
		Body: []byte(renderStatusPage()),
	}, nil
}

func jsonResponse(status int, value any) pluginapi.ManagementResponse {
	body, _ := json.Marshal(value)
	return pluginapi.ManagementResponse{
		StatusCode: status,
		Headers: http.Header{
			"Content-Type":  []string{"application/json; charset=utf-8"},
			"Cache-Control": []string{"no-store"},
		},
		Body: body,
	}
}

func renderStatusPage() string {
	var out bytes.Buffer
	out.WriteString(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Codex Quota Warmup</title>`)
	out.WriteString(`<style>body{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;max-width:1100px;margin:32px auto;padding:0 18px;line-height:1.45}h1{font-size:24px}.bar{display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin:14px 0}input,button{font:inherit;padding:8px 10px}input{min-width:300px}button{cursor:pointer}table{border-collapse:collapse;width:100%;font-size:14px}th,td{text-align:left;padding:8px;border-bottom:1px solid #ddd}code,pre{font-family:ui-monospace,monospace}pre{white-space:pre-wrap;padding:10px;background:#f6f6f6;border-radius:6px}.muted{opacity:.7;font-size:13px}.ok{font-weight:600}</style></head><body>`)
	out.WriteString(`<h1>Codex Quota Warmup</h1><p class="muted">The public plugin page contains no account data. Enter the CPA Management Key to load authenticated status. The key is kept only in this tab's sessionStorage.</p>`)
	out.WriteString(`<div class="bar"><input id="key" type="password" autocomplete="off" placeholder="Management Key"><button onclick="saveKey()">Load</button><button onclick="checkNow()">Check Now</button><button onclick="testTG()">Test Telegram</button></div>`)
	out.WriteString(`<div id="summary"></div><table><thead><tr><th>Account</th><th>5h remaining</th><th>Reset</th><th>Status</th><th>Last warm</th><th>Action</th></tr></thead><tbody id="accounts"><tr><td colspan="6">Enter Management Key.</td></tr></tbody></table><pre id="result"></pre>`)
	out.WriteString(`<script>
const base='/v0/management/plugins/` + html.EscapeString(pluginID) + `';
const keyInput=document.getElementById('key'), result=document.getElementById('result');
keyInput.value=sessionStorage.getItem('cpa-mgmt-key')||'';
function headers(){return {'Authorization':'Bearer '+keyInput.value.trim(),'Content-Type':'application/json'}}
function saveKey(){sessionStorage.setItem('cpa-mgmt-key',keyInput.value.trim());loadStatus()}
function esc(v){return String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function fmtTime(v){return v?new Date(v).toLocaleString():'-'}
async function api(path,opt={}){const r=await fetch(base+path,{...opt,headers:{...headers(),...(opt.headers||{})}});const t=await r.text();let data;try{data=JSON.parse(t)}catch{data={raw:t}};if(!r.ok)throw new Error(data.error||data.message||t||('HTTP '+r.status));return data}
async function loadStatus(){try{const s=await api('/status');document.getElementById('summary').innerHTML='<p><b>Version</b> '+esc(s.version)+' &nbsp; <b>Model</b> '+esc(s.model)+' &nbsp; <b>Interval</b> '+esc(s.interval)+' &nbsp; <b>Running</b> '+esc(s.running)+' &nbsp; <b>Telegram</b> '+(s.telegram_configured?'configured':'off')+'</p>';const rows=(s.accounts||[]).map(a=>{const encoded=encodeURIComponent(a.auth_id||'');return '<tr><td>'+esc(a.email||a.name)+'</td><td>'+(a.remaining_percent==null?'-':Number(a.remaining_percent).toFixed(2)+'%')+'</td><td>'+esc(fmtTime(a.reset_at))+'</td><td>'+esc(a.status)+(a.error?'<br><span class="muted">'+esc(a.error)+'</span>':'')+'</td><td>'+esc(fmtTime(a.last_warm_at))+'</td><td><button data-auth="'+encoded+'" onclick="pingEncoded(this.dataset.auth)">Ping</button></td></tr>'}).join('');document.getElementById('accounts').innerHTML=rows||'<tr><td colspan="6">No Codex accounts found yet.</td></tr>';result.textContent='';}catch(e){result.textContent=String(e)}}
async function checkNow(){try{result.textContent=JSON.stringify(await api('/check',{method:'POST'}),null,2);setTimeout(loadStatus,1500)}catch(e){result.textContent=String(e)}}
function pingEncoded(encoded){return pingNow(decodeURIComponent(encoded||''))}
async function pingNow(id){try{result.textContent=JSON.stringify(await api('/ping',{method:'POST',body:JSON.stringify({auth_id:id})}),null,2);setTimeout(loadStatus,1500)}catch(e){result.textContent=String(e)}}
async function testTG(){try{result.textContent=JSON.stringify(await api('/telegram/test',{method:'POST'}),null,2)}catch(e){result.textContent=String(e)}}
if(keyInput.value)loadStatus();
</script></body></html>`)
	return out.String()
}
