package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func sendTelegram(ctx context.Context, cfg pluginConfig, message string) error {
	if !telegramConfigured(cfg) {
		return fmt.Errorf("Telegram is not configured")
	}
	payload, err := json.Marshal(map[string]any{
		"chat_id":                  cfg.TelegramChatID,
		"text":                     message,
		"disable_web_page_preview": true,
	})
	if err != nil {
		return err
	}

	endpoint := "https://api.telegram.org/bot" + cfg.TelegramBotToken + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Telegram returned HTTP %d: %s", resp.StatusCode, compactBytes(body, 500))
	}
	return nil
}
