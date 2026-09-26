# CPA Codex Quota Warmup

A CLIProxyAPI native plugin that periodically checks each Codex OAuth account's current 5-hour quota through CPA and sends one tiny, account-pinned request when that quota is fully available. It can notify Telegram and exposes a status page in the official CPA Management Center.

## Decision model

CPA quota is the source of truth.

Every `interval` (default `30m`) the plugin:

1. Lists enabled Codex credentials from CPA.
2. Performs a fresh Codex quota probe through CPA host callbacks.
3. Reads the current 5-hour window.
4. If the live quota is not full, it does nothing.
5. If the live quota is full and this continuous full state has not already triggered, it sends one tiny request through CPA, pinned to that exact `AuthID`.
6. It keeps only an in-memory transition guard so the same still-full observation does not repeatedly ping when upstream quota percentages are slow to update.

The plugin does **not** persist quota values, does not infer that quota is full from elapsed time, and does not need a separate `/data` volume or state file.

If CPA/plugin process restarts while the upstream quota still reports 100% available, the first fresh check after restart can trigger once again. That is an intentional tradeoff for keeping the plugin stateless on disk.

## Why this design

- Keeps upstream CLIProxyAPI and the official Management Center unchanged.
- Uses CPA's native plugin ABI and host callbacks.
- Quota decisions are based on a fresh upstream quota result obtained through CPA, not on locally persisted state.
- The warm-up request uses CPA's own `host.model.execute`, `ForcedProvider=codex`, and the exact `AuthID`.
- Does not write or modify CPA auth files.
- Does not persist OAuth tokens or quota state.

## Plugin Store source

Add the registry to CPA:

```yaml
plugins:
  enabled: true
  dir: plugins
  store-sources:
    - https://raw.githubusercontent.com/madwind/cpa-plugin-codex-quota-warmup/main/registry.json
```

The official CPA plugin source remains enabled; `store-sources` only appends this source.

Then install **Codex Quota Warmup** from the CPA Plugin Store and restart CPA if the UI says a restart is required.

## Configuration

Only enabling the plugin is required:

```yaml
plugins:
  configs:
    codex-quota-warmup:
      enabled: true
```

Optional overrides:

```yaml
plugins:
  configs:
    codex-quota-warmup:
      enabled: true
      interval: "30m"
      initial_delay: "15s"
      model: "gpt-5.6-luna"
      ping_text: "ping"
      max_output_tokens: 16
      full_used_percent: 0
      notify_success: true
      notify_failure: true
      notify_poll_failures: false
```

### Telegram

Prefer environment variables so the bot token does not need to live in CPA YAML:

```text
CPA_CODEX_WARMUP_TELEGRAM_BOT_TOKEN
CPA_CODEX_WARMUP_TELEGRAM_CHAT_ID
```

The YAML keys `telegram_bot_token` and `telegram_chat_id` are also accepted.

## Management UI

When enabled, the official CPA Management Center can expose the plugin resource menu **Codex Quota Warmup**.

The public resource page contains no quota/account data. It asks for the CPA Management Key and keeps it only in the browser tab's `sessionStorage`, then calls the authenticated plugin Management API.

Endpoints:

```text
GET  /v0/management/plugins/codex-quota-warmup/status
POST /v0/management/plugins/codex-quota-warmup/check
POST /v0/management/plugins/codex-quota-warmup/ping
POST /v0/management/plugins/codex-quota-warmup/telegram/test

GET  /v0/resource/plugins/codex-quota-warmup/status
```

`POST .../ping` body:

```json
{"auth_id":"<CPA auth ID>"}
```

## CPA quota API note

CLIProxyAPI v7.3.18 does not currently expose a `host.quota.fetch` callback that lets one native plugin invoke another registered quota provider directly.

The official Management Center's Codex quota page also obtains current usage by asking CPA to make an authenticated request to `https://chatgpt.com/backend-api/wham/usage`. This plugin follows the same live-probe principle through CPA host callbacks: it gets the credential from CPA, has CPA perform the HTTP request, and makes the warm-up decision only from that fresh response.

The warm-up model request itself is executed entirely through CPA's Codex executor and exact `AuthID`.

### Per-credential proxy note

The warm-up model request follows the selected credential's CPA executor/routing behavior. The current generic `host.http.do` callback used for the quota probe does not expose a credential-specific proxy argument. If different Codex credentials are intentionally pinned to different proxies, quota probing needs an additional CPA host capability or another auth-scoped request path to preserve that distinction.

## Build

The repository workflow currently builds only:

- linux/amd64

Local Linux build:

```bash
CGO_ENABLED=1 go build -buildmode=c-shared -trimpath -o codex-quota-warmup.so .
rm -f codex-quota-warmup.h
```

To publish `0.1.1`, push tag:

```bash
git tag v0.1.1
git push origin v0.1.1
```

GitHub Actions creates:

```text
codex-quota-warmup_0.1.1_linux_amd64.zip
checksums.txt
```

## Compatibility

The initial release targets the CPA v7 plugin ABI available in CLIProxyAPI v7.3.18. The binary communicates with CPA through the C ABI/JSON RPC host-callback boundary rather than Go's `plugin` package.
