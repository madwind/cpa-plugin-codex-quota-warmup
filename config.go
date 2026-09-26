package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

type pluginConfig struct {
	Interval           string  `yaml:"interval"`
	InitialDelay       string  `yaml:"initial_delay"`
	Model              string  `yaml:"model"`
	PingText           string  `yaml:"ping_text"`
	MaxOutputTokens    int     `yaml:"max_output_tokens"`
	FullUsedPercent    float64 `yaml:"full_used_percent"`
	MinWarmInterval    string  `yaml:"min_warm_interval"`
	StateFile          string  `yaml:"state_file"`
	TelegramBotToken   string  `yaml:"telegram_bot_token"`
	TelegramChatID     string  `yaml:"telegram_chat_id"`
	NotifySuccess      *bool   `yaml:"notify_success"`
	NotifyFailure      *bool   `yaml:"notify_failure"`
	NotifyPollFailures bool    `yaml:"notify_poll_failures"`

	intervalDuration     time.Duration
	initialDelayDuration time.Duration
	minWarmDuration      time.Duration
	notifySuccessValue   bool
	notifyFailureValue   bool
}

var currentConfig atomic.Value

func defaultConfig() pluginConfig {
	yes := true
	return pluginConfig{
		Interval:        "30m",
		InitialDelay:    "15s",
		Model:           "gpt-5.6-luna",
		PingText:        "ping",
		MaxOutputTokens: 16,
		FullUsedPercent: 0,
		MinWarmInterval: "4h45m",
		NotifySuccess:   &yes,
		NotifyFailure:   &yes,
	}
}

func configurePlugin(raw []byte) error {
	var req lifecycleRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return err
		}
	}

	cfg := defaultConfig()
	if len(req.ConfigYAML) > 0 {
		if err := yaml.Unmarshal(req.ConfigYAML, &cfg); err != nil {
			return err
		}
	}
	if err := cfg.normalize(); err != nil {
		return err
	}
	currentConfig.Store(cfg)
	restartWorker(cfg)
	return nil
}

func (cfg *pluginConfig) normalize() error {
	cfg.Interval = strings.TrimSpace(cfg.Interval)
	cfg.InitialDelay = strings.TrimSpace(cfg.InitialDelay)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.PingText = strings.TrimSpace(cfg.PingText)
	cfg.MinWarmInterval = strings.TrimSpace(cfg.MinWarmInterval)
	cfg.StateFile = strings.TrimSpace(cfg.StateFile)
	cfg.TelegramBotToken = strings.TrimSpace(cfg.TelegramBotToken)
	cfg.TelegramChatID = strings.TrimSpace(cfg.TelegramChatID)

	if cfg.Interval == "" {
		cfg.Interval = "30m"
	}
	if cfg.InitialDelay == "" {
		cfg.InitialDelay = "15s"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-5.6-luna"
	}
	if cfg.PingText == "" {
		cfg.PingText = "ping"
	}
	if cfg.MaxOutputTokens == 0 {
		cfg.MaxOutputTokens = 16
	}
	if cfg.MinWarmInterval == "" {
		cfg.MinWarmInterval = "4h45m"
	}

	if cfg.TelegramBotToken == "" {
		cfg.TelegramBotToken = strings.TrimSpace(os.Getenv("CPA_CODEX_WARMUP_TELEGRAM_BOT_TOKEN"))
	}
	if cfg.TelegramChatID == "" {
		cfg.TelegramChatID = strings.TrimSpace(os.Getenv("CPA_CODEX_WARMUP_TELEGRAM_CHAT_ID"))
	}

	if cfg.MaxOutputTokens < 1 || cfg.MaxOutputTokens > 1024 {
		return errors.New("max_output_tokens must be between 1 and 1024")
	}
	if cfg.FullUsedPercent < 0 || cfg.FullUsedPercent > 100 {
		return errors.New("full_used_percent must be between 0 and 100")
	}

	var err error
	cfg.intervalDuration, err = time.ParseDuration(cfg.Interval)
	if err != nil || cfg.intervalDuration < time.Minute {
		return errors.New("interval must be a valid duration of at least 1m")
	}
	cfg.initialDelayDuration, err = time.ParseDuration(cfg.InitialDelay)
	if err != nil || cfg.initialDelayDuration < 0 {
		return errors.New("initial_delay must be a valid non-negative duration")
	}
	cfg.minWarmDuration, err = time.ParseDuration(cfg.MinWarmInterval)
	if err != nil || cfg.minWarmDuration < 0 {
		return errors.New("min_warm_interval must be a valid non-negative duration")
	}

	cfg.notifySuccessValue = cfg.NotifySuccess == nil || *cfg.NotifySuccess
	cfg.notifyFailureValue = cfg.NotifyFailure == nil || *cfg.NotifyFailure
	return nil
}

func loadedConfig() pluginConfig {
	if raw := currentConfig.Load(); raw != nil {
		if cfg, ok := raw.(pluginConfig); ok {
			return cfg
		}
	}
	cfg := defaultConfig()
	_ = cfg.normalize()
	return cfg
}

func telegramConfigured(cfg pluginConfig) bool {
	return cfg.TelegramBotToken != "" && cfg.TelegramChatID != ""
}
