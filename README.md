# CPA Codex Quota Warmup

A CLIProxyAPI native plugin that polls each Codex OAuth account's 5-hour quota and sends one tiny, account-pinned request after the quota window is fully available again. It can notify Telegram and exposes a status page in the official CPA Management Center.

## Why this design

- Keeps upstream CLIProxyAPI and the official Management Center unchanged.
- Uses CPA's native plugin ABI and host callbacks.
- Uses `host.auth.list` / `host.auth.get` only to query the Codex quota endpoint.
- The real warm-up request uses CPA's own `host.model.execute`, `ForcedProvider=codex`, and the exact `AuthID`, so it follows CPA's normal Codex executor/OAuth path and cannot randomly hit another account.
- Does not write or modify CPA auth files.
- Persistent state contains only the auth key, last successful warm-up time, and reset-cycle key; it never stores OAuth tokens.

## Behavior

Every `interval` (default `30m`) the plugin:

1. Lists enabled Codex credentials from CPA.
2. Reads each credential's access token transiently in memory.
3. Queries `https://chatgpt.com/backend-api/wham/usage` through CPA's host HTTP callback.
4. Selects the rate-limit window closest to five hours.
5. If `used_percent <= full_used_percent` (default `0`) and the account is allowed/not exhausted, it checks duplicate guards.
6. Sends a tiny Responses request through CPA, pinned to the exact auth ID.
7. Stores the successful cycle marker and optionally sends a Telegram notification.

The `min_warm_interval` guard (default `4h45m`) protects against quota percentages rounding back to `0% used` immediately after a tiny warm-up request.

## Plugin Store source

After this repository exists and has a release, add its registry to CPA:

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
      min_warm_interval: "4h45m"
      state_file: "/data/codex-quota-warmup-state.json"
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

## State file

For containers, mount the configured state path on persistent storage. Example:

```yaml
state_file: "/data/codex-quota-warmup-state.json"
```

Stored data is intentionally minimal:

```json
{
  "accounts": {
    "<auth-id>": {
      "last_warm_at": "2026-09-26T12:34:56Z",
      "last_reset_key": "reset:1790425800"
    }
  }
}
```

No access token, refresh token, account token, Management Key, or Telegram token is written to this file.

## Build

The repository workflow currently builds only:

- linux/amd64

Local Linux build:

```bash
CGO_ENABLED=1 go build -buildmode=c-shared -trimpath -o codex-quota-warmup.so .
rm -f codex-quota-warmup.h
```

To publish `0.1.0`, push tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions creates the standard CPA Plugin Store release assets:

```text
codex-quota-warmup_0.1.0_linux_amd64.zip
checksums.txt
```

## Compatibility

The initial release targets the CPA v7 plugin ABI available in CLIProxyAPI v7.3.18. The binary communicates with CPA through the C ABI/JSON RPC host-callback boundary rather than Go's `plugin` package.

### Quota request proxy note

The warm-up model request is executed by CPA for the exact credential, so CPA owns normal Codex routing/executor behavior. The lightweight `wham/usage` polling request is made through CPA's generic host HTTP callback; current CPA does not expose a credential-specific proxy argument for that callback.
