package main

import (
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
			{Method: http.MethodGet, Path: "/plugins/" + pluginID + "/status", Description: "Return quota warm-up status."},
			{Method: http.MethodPost, Path: "/plugins/" + pluginID + "/check", Description: "Run an immediate quota check."},
			{Method: http.MethodPost, Path: "/plugins/" + pluginID + "/ping", Description: "Force a warm-up request for one Codex auth."},
			{Method: http.MethodPost, Path: "/plugins/" + pluginID + "/telegram/test", Description: "Send a Telegram test message."},
		},
		Resources: []pluginapi.ResourceRoute{
			{Path: "/status", Menu: "Codex Quota Warmup", Description: "Codex quota status, manual check, warm-up and Telegram diagnostics."},
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
	return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Codex Quota Warmup</title>
<style>
:root{
  --bg-secondary:#faf9f5;--bg-primary:#f0eee8;--bg-tertiary:#e9e6df;--bg-hover:var(--bg-tertiary);
  --text-primary:#2d2a26;--text-secondary:#6d6760;--text-tertiary:#a29c95;
  --border-color:#e3e1db;--border-primary:#d5d2cb;--primary-color:#8b8680;--primary-hover:#7f7a74;
  --primary-contrast:#fff;--success-color:#10b981;--warning-color:#c65746;--shadow:0 1px 2px rgb(0 0 0 / .08);
  --radius-md:8px
}
[data-theme="white"]{
  --bg-secondary:#fff;--bg-primary:#fff;--bg-tertiary:#f6f6f6;--text-primary:#2d2a26;--text-secondary:#6d6760;
  --text-tertiary:#a29c95;--border-color:#e5e5e5;--border-primary:#d9d9d9;--primary-color:#8b8680;--primary-hover:#7f7a74
}
[data-theme="dark"]{
  --bg-secondary:#151412;--bg-primary:#1d1b18;--bg-tertiary:#262320;--bg-hover:#2e2a26;
  --text-primary:#f6f4f1;--text-secondary:#c9c3bb;--text-tertiary:#9c958d;--border-color:#3a3530;
  --border-primary:#4a453f;--primary-color:#8b8680;--primary-hover:#9a948e;--success-color:#10b981;--warning-color:#c65746;
  --shadow:0 1px 3px rgb(0 0 0 / .3)
}
*{box-sizing:border-box}
html,body{min-height:100%;margin:0;background:var(--bg-secondary);color:var(--text-primary)}
body{font-family:Inter,ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;font-size:14px;line-height:1.5}
button,input{font:inherit}
.page{width:min(1180px,100%);margin:0 auto;padding:30px clamp(18px,3vw,42px) 48px}
.header{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-bottom:22px}
.title{margin:0;font-size:26px;line-height:1.2;font-weight:700;letter-spacing:-.02em}
.subtitle{margin:7px 0 0;color:var(--text-secondary)}
.toolbar{display:flex;gap:8px;flex-wrap:wrap}
.card{background:var(--bg-primary);border:1px solid var(--border-color);border-radius:12px;box-shadow:var(--shadow)}
.auth{display:none;padding:16px;margin-bottom:18px}
.auth.show{display:block}
.auth-title{font-weight:650;margin-bottom:5px}
.auth-desc{color:var(--text-secondary);font-size:13px;margin-bottom:12px}
.auth-row{display:flex;gap:8px;align-items:center;flex-wrap:wrap}
.input{min-width:min(360px,100%);flex:1 1 280px;border:1px solid var(--border-primary);background:var(--bg-secondary);color:var(--text-primary);border-radius:8px;padding:9px 11px;outline:none}
.input:focus{border-color:var(--primary-color);box-shadow:0 0 0 3px color-mix(in srgb,var(--primary-color) 15%,transparent)}
.btn{border:1px solid var(--border-primary);background:var(--bg-primary);color:var(--text-primary);border-radius:8px;padding:8px 12px;cursor:pointer;transition:background .16s ease,border-color .16s ease}
.btn:hover{background:var(--bg-hover);border-color:var(--primary-color)}
.btn.primary{background:var(--primary-color);border-color:var(--primary-color);color:var(--primary-contrast)}
.btn.primary:hover{background:var(--primary-hover)}
.btn:disabled{opacity:.55;cursor:not-allowed}
.summary{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px;margin-bottom:18px}
.metric{padding:15px 16px}
.metric-label{font-size:12px;color:var(--text-secondary);margin-bottom:5px}
.metric-value{font-size:17px;font-weight:650;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.accounts{overflow:hidden}
.accounts-head{display:flex;align-items:center;justify-content:space-between;padding:15px 16px;border-bottom:1px solid var(--border-color)}
.accounts-title{font-size:15px;font-weight:650}
.table-wrap{overflow:auto}
table{width:100%;border-collapse:collapse;min-width:820px}
th,td{text-align:left;padding:12px 16px;border-bottom:1px solid var(--border-color);vertical-align:middle}
th{font-size:12px;color:var(--text-secondary);font-weight:600;background:color-mix(in srgb,var(--bg-tertiary) 55%,transparent)}
tbody tr:last-child td{border-bottom:0}
.account{font-weight:550}
.muted{color:var(--text-secondary);font-size:12px}
.pill{display:inline-flex;align-items:center;gap:6px;border:1px solid var(--border-color);background:var(--bg-tertiary);border-radius:999px;padding:3px 8px;font-size:12px}
.dot{width:7px;height:7px;border-radius:50%;background:var(--text-tertiary)}
.dot.ok{background:var(--success-color)}
.dot.warn{background:var(--warning-color)}
.notice{display:none;margin-top:14px;padding:11px 13px;border:1px solid var(--border-color);border-radius:8px;background:var(--bg-primary);white-space:pre-wrap;word-break:break-word}
.notice.show{display:block}
.notice.error{border-color:color-mix(in srgb,var(--warning-color) 45%,var(--border-color));color:var(--warning-color)}
.empty{padding:26px 16px;text-align:center;color:var(--text-secondary)}
@media(max-width:820px){.header{flex-direction:column}.summary{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:520px){.summary{grid-template-columns:1fr}.page{padding-left:14px;padding-right:14px}}
</style>
</head>
<body>
<div class="page">
  <div class="header">
    <div>
      <h1 class="title">Codex Quota Warmup</h1>
      <p class="subtitle">Live 5-hour quota status and warm-up controls.</p>
    </div>
    <div class="toolbar">
      <button class="btn" id="checkBtn" onclick="checkNow()">Check Now</button>
      <button class="btn" id="tgBtn" onclick="testTG()">Test Telegram</button>
    </div>
  </div>

  <section class="card auth" id="authBox">
    <div class="auth-title">Management authentication required</div>
    <div class="auth-desc" id="authDesc">CPA login credentials could not be reused automatically. Enter the Management Key once for this tab.</div>
    <div class="auth-row">
      <input class="input" id="key" type="password" autocomplete="off" placeholder="Management Key">
      <button class="btn primary" onclick="saveKey()">Connect</button>
    </div>
  </section>

  <section class="summary" id="summary">
    <div class="card metric"><div class="metric-label">Version</div><div class="metric-value" id="mVersion">-</div></div>
    <div class="card metric"><div class="metric-label">Model</div><div class="metric-value" id="mModel">-</div></div>
    <div class="card metric"><div class="metric-label">Interval</div><div class="metric-value" id="mInterval">-</div></div>
    <div class="card metric"><div class="metric-label">Runtime</div><div class="metric-value" id="mRuntime">-</div></div>
  </section>

  <section class="card accounts">
    <div class="accounts-head">
      <div class="accounts-title">Codex accounts</div>
      <span class="pill"><span class="dot" id="stateDot"></span><span id="stateText">Connecting</span></span>
    </div>
    <div class="table-wrap">
      <table>
        <thead><tr><th>Account</th><th>5h remaining</th><th>Reset</th><th>Status</th><th>Last warm</th><th>Action</th></tr></thead>
        <tbody id="accounts"><tr><td colspan="6" class="empty">Loading...</td></tr></tbody>
      </table>
    </div>
  </section>
  <div class="notice" id="notice"></div>
</div>
<script>
const base='/v0/management/plugins/` + html.EscapeString(pluginID) + `';
const AUTH_STORAGE='cli-proxy-auth';
const LEGACY_AUTH_STORAGE='managementKey';
const SESSION_KEY='cpa-mgmt-key';
const ENC_PREFIX='enc::v1::';
const SECRET_SALT='cli-proxy-api-webui::secure-storage';
const keyInput=document.getElementById('key');
const authBox=document.getElementById('authBox');
const authDesc=document.getElementById('authDesc');
const notice=document.getElementById('notice');
let currentKey='';
let keySource='';

function applyTheme(theme){
  if(theme==='dark'||theme==='white') document.documentElement.setAttribute('data-theme',theme);
  else document.documentElement.removeAttribute('data-theme');
}
function syncTheme(){
  let theme='';
  try{
    if(window.parent!==window && window.parent.location.origin===window.location.origin){
      theme=window.parent.document.documentElement.getAttribute('data-theme')||'';
    }
  }catch(_){}
  if(!theme && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) theme='dark';
  applyTheme(theme);
}
syncTheme();
try{
  if(window.parent!==window && window.parent.location.origin===window.location.origin){
    new MutationObserver(syncTheme).observe(window.parent.document.documentElement,{attributes:true,attributeFilter:['data-theme']});
  }
}catch(_){}

function decodeCPAStorage(raw){
  if(!raw) return null;
  let text=raw;
  if(raw.startsWith(ENC_PREFIX)){
    const encoded=raw.slice(ENC_PREFIX.length);
    const binary=atob(encoded);
    const encrypted=new Uint8Array(binary.length);
    for(let i=0;i<binary.length;i++) encrypted[i]=binary.charCodeAt(i);
    const keyBytes=new TextEncoder().encode(SECRET_SALT+'|'+window.location.host+'|'+navigator.userAgent);
    const clear=new Uint8Array(encrypted.length);
    for(let i=0;i<encrypted.length;i++) clear[i]=encrypted[i]^keyBytes[i%keyBytes.length];
    text=new TextDecoder().decode(clear);
  }
  try{return JSON.parse(text)}catch(_){return text}
}
function extractManagementKey(value){
  if(!value) return '';
  if(typeof value==='string') return value;
  const state=value.state||value;
  return typeof state.managementKey==='string'?state.managementKey:'';
}
function readSavedCPAKey(){
  try{
    let value=decodeCPAStorage(localStorage.getItem(AUTH_STORAGE));
    let key=extractManagementKey(value);
    if(key) return key.trim();
    value=decodeCPAStorage(localStorage.getItem(LEGACY_AUTH_STORAGE));
    key=extractManagementKey(value);
    return key.trim();
  }catch(_){return ''}
}
function bootstrapKey(){
  const session=(sessionStorage.getItem(SESSION_KEY)||'').trim();
  if(session){currentKey=session;keySource='session';return}
  const saved=readSavedCPAKey();
  if(saved){currentKey=saved;keySource='cpa';return}
  currentKey='';keySource='';
}
function showAuth(message){
  authDesc.textContent=message||'CPA login credentials could not be reused automatically. Enter the Management Key once for this tab.';
  keyInput.value=currentKey||'';
  authBox.classList.add('show');
}
function hideAuth(){authBox.classList.remove('show')}
function saveKey(){
  const key=keyInput.value.trim();
  if(!key){showAuth('Enter the Management Key to continue.');return}
  currentKey=key;keySource='session';sessionStorage.setItem(SESSION_KEY,key);hideAuth();loadStatus();
}
function headers(){return {'Authorization':'Bearer '+currentKey,'Content-Type':'application/json'}}
function esc(v){return String(v??'').replace(/[&<>"']/g,function(ch){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch]})}
function fmtTime(v){return v?new Date(v).toLocaleString():'-'}
function setNotice(text,isError){
  notice.textContent=text||'';
  notice.classList.toggle('show',Boolean(text));
  notice.classList.toggle('error',Boolean(isError));
}
function setState(text,kind){
  document.getElementById('stateText').textContent=text;
  const dot=document.getElementById('stateDot');
  dot.className='dot'+(kind?' '+kind:'');
}
function statusPill(status,error){
  const lower=String(status||'').toLowerCase();
  const kind=(lower.includes('error')||lower.includes('fail'))?'warn':(lower.includes('warm')||lower.includes('waiting')||lower.includes('trigger')||lower.includes('ok'))?'ok':'';
  return '<span class="pill"><span class="dot '+kind+'"></span>'+esc(status||'-')+'</span>'+(error?'<div class="muted">'+esc(error)+'</div>':'');
}
async function api(path,opt={}){
  if(!currentKey) throw new Error('Management Key is required');
  const r=await fetch(base+path,{...opt,headers:{...headers(),...(opt.headers||{})}});
  const t=await r.text();
  let data;try{data=JSON.parse(t)}catch(_){data={raw:t}}
  if(!r.ok){
    if(r.status===401){
      if(keySource==='session') sessionStorage.removeItem(SESSION_KEY);
      currentKey='';keySource='';
      showAuth('The saved Management Key was rejected. Enter the current key once for this tab.');
    }
    throw new Error(data.error||data.message||t||('HTTP '+r.status));
  }
  return data;
}
async function loadStatus(){
  if(!currentKey){setState('Authentication required','warn');showAuth();document.getElementById('accounts').innerHTML='<tr><td colspan="6" class="empty">Management authentication required.</td></tr>';return}
  hideAuth();setState('Loading','');
  try{
    const s=await api('/status');
    document.getElementById('mVersion').textContent=s.version||'-';
    document.getElementById('mModel').textContent=s.model||'-';
    document.getElementById('mInterval').textContent=s.interval||'-';
    document.getElementById('mRuntime').textContent=(s.running?'Running':'Idle')+' / Telegram '+(s.telegram_configured?'On':'Off');
    const rows=(s.accounts||[]).map(function(a){
      const encoded=encodeURIComponent(a.auth_id||'');
      const remaining=a.remaining_percent==null?'-':Number(a.remaining_percent).toFixed(2)+'%';
      return '<tr><td><div class="account">'+esc(a.email||a.name||a.auth_id)+'</div></td><td>'+remaining+'</td><td>'+esc(fmtTime(a.reset_at))+'</td><td>'+statusPill(a.status,a.error)+'</td><td>'+esc(fmtTime(a.last_warm_at))+'</td><td><button class="btn" data-auth="'+encoded+'" onclick="pingEncoded(this.dataset.auth)">Ping</button></td></tr>';
    }).join('');
    document.getElementById('accounts').innerHTML=rows||'<tr><td colspan="6" class="empty">No Codex accounts found yet.</td></tr>';
    setState('Connected','ok');setNotice('',false);
  }catch(e){setState('Unavailable','warn');setNotice(String(e),true)}
}
async function checkNow(){
  try{setNotice('Check requested...',false);await api('/check',{method:'POST'});setTimeout(loadStatus,1200)}
  catch(e){setNotice(String(e),true)}
}
function pingEncoded(encoded){return pingNow(decodeURIComponent(encoded||''))}
async function pingNow(id){
  try{setNotice('Ping requested...',false);await api('/ping',{method:'POST',body:JSON.stringify({auth_id:id})});setTimeout(loadStatus,1200)}
  catch(e){setNotice(String(e),true)}
}
async function testTG(){
  try{setNotice('Sending Telegram test...',false);await api('/telegram/test',{method:'POST'});setNotice('Telegram test sent.',false)}
  catch(e){setNotice(String(e),true)}
}
bootstrapKey();
if(currentKey) hideAuth(); else showAuth();
loadStatus();
</script>
</body>
</html>`
}
