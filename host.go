package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type hostEnvelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type hostAuthListResponse struct {
	Files []pluginapi.HostAuthFileEntry `json:"files"`
}

type authMaterial struct {
	AccessToken string
	AccountID   string
}

func callHost(ctx context.Context, method string, payload any, target any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal host callback %s: %w", method, err)
	}

	code, responseBytes, err := callHostRaw(method, raw)
	if err != nil {
		return err
	}
	if len(responseBytes) == 0 {
		return fmt.Errorf("host callback %s returned no response, code=%d", method, code)
	}

	var env hostEnvelope
	if err := json.Unmarshal(responseBytes, &env); err != nil {
		return fmt.Errorf("decode host callback %s: %w", method, err)
	}
	if !env.OK {
		if env.Error != nil {
			return fmt.Errorf("%s: %s", env.Error.Code, env.Error.Message)
		}
		return fmt.Errorf("host callback %s failed", method)
	}
	if code != 0 {
		return fmt.Errorf("host callback %s returned code=%d", method, code)
	}
	if target != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, target); err != nil {
			return fmt.Errorf("decode host callback result %s: %w", method, err)
		}
	}
	return nil
}

func listCodexAuths(ctx context.Context) ([]pluginapi.HostAuthFileEntry, error) {
	var resp hostAuthListResponse
	if err := callHost(ctx, pluginabi.MethodHostAuthList, map[string]any{}, &resp); err != nil {
		return nil, err
	}
	out := make([]pluginapi.HostAuthFileEntry, 0, len(resp.Files))
	for _, auth := range resp.Files {
		provider := strings.TrimSpace(auth.Provider)
		if provider == "" {
			provider = strings.TrimSpace(auth.Type)
		}
		if strings.EqualFold(provider, "codex") {
			out = append(out, auth)
		}
	}
	return out, nil
}

func getAuthMaterial(ctx context.Context, authIndex string) (authMaterial, error) {
	var resp pluginapi.HostAuthGetResponse
	if err := callHost(ctx, pluginabi.MethodHostAuthGet, pluginapi.HostAuthGetRequest{AuthIndex: authIndex}, &resp); err != nil {
		return authMaterial{}, err
	}
	if len(resp.JSON) == 0 {
		return authMaterial{}, fmt.Errorf("empty auth JSON")
	}
	return parseAuthMaterial(resp.JSON)
}

func parseAuthMaterial(raw []byte) (authMaterial, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return authMaterial{}, fmt.Errorf("invalid auth JSON: %w", err)
	}

	token := firstString(root, "access_token", "accessToken", "oauth_access_token", "oauthAccessToken", "token")
	accountID := firstString(root, "account_id", "chatgpt_account_id", "accountId", "chatgptAccountId")
	for _, key := range []string{"tokens", "credentials", "auth", "oauth", "session"} {
		nestedRaw, ok := root[key]
		if !ok {
			continue
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(nestedRaw, &nested) != nil {
			continue
		}
		if token == "" {
			token = firstString(nested, "access_token", "accessToken", "oauth_access_token", "oauthAccessToken", "token")
		}
		if accountID == "" {
			accountID = firstString(nested, "account_id", "chatgpt_account_id", "accountId", "chatgptAccountId")
		}
	}
	if token == "" {
		return authMaterial{}, fmt.Errorf("missing Codex access token")
	}
	return authMaterial{AccessToken: token, AccountID: accountID}, nil
}

func firstString(values map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func hostHTTP(ctx context.Context, request pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	var resp pluginapi.HTTPResponse
	if err := callHost(ctx, pluginabi.MethodHostHTTPDo, request, &resp); err != nil {
		return pluginapi.HTTPResponse{}, err
	}
	return resp, nil
}

func executeWarmup(ctx context.Context, cfg pluginConfig, auth pluginapi.HostAuthFileEntry) error {
	if strings.TrimSpace(auth.ID) == "" {
		return fmt.Errorf("auth %s has no ID", authDisplay(auth))
	}

	body, err := json.Marshal(map[string]any{
		"model":             cfg.Model,
		"input":             cfg.PingText,
		"max_output_tokens": cfg.MaxOutputTokens,
		"store":             false,
		"stream":            false,
	})
	if err != nil {
		return err
	}

	req := pluginapi.HostModelExecutionRequest{
		EntryProtocol:  "openai-response",
		ExitProtocol:   "openai-response",
		Model:          cfg.Model,
		Stream:         false,
		Body:           body,
		Headers:        http.Header{"Content-Type": []string{"application/json"}},
		ForcedProvider: "codex",
		AuthID:         auth.ID,
	}
	var resp pluginapi.HostModelExecutionResponse
	if err := callHost(ctx, pluginabi.MethodHostModelExecute, req, &resp); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("host model execution returned HTTP %d: %s", resp.StatusCode, compactBytes(resp.Body, 500))
	}
	return nil
}

func authDisplay(auth pluginapi.HostAuthFileEntry) string {
	if email := strings.TrimSpace(auth.Email); email != "" {
		return email
	}
	if label := strings.TrimSpace(auth.Label); label != "" {
		return label
	}
	if name := strings.TrimSpace(auth.Name); name != "" {
		return name
	}
	if id := strings.TrimSpace(auth.ID); id != "" {
		return id
	}
	return strings.TrimSpace(auth.AuthIndex)
}
