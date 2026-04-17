package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/muszkin/slack-presence-automation/internal/config"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvSlackAppToken, "xapp-1-ABCDEF")
	t.Setenv(config.EnvSlackBotToken, "xoxb-1-ABCDEF")
	t.Setenv(config.EnvSlackUserToken, "xoxp-1-ABCDEF")
	t.Setenv(config.EnvGoogleCredentials, `{"type":"service_account"}`)
	t.Setenv(config.EnvDatabasePath, "/tmp/presence.db")
}

func TestLoadAppliesDefaultsForOptionalVars(t *testing.T) {
	setValidEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GoogleCalendarID != "primary" {
		t.Errorf("GoogleCalendarID default = %q, want primary", cfg.GoogleCalendarID)
	}
	if cfg.TickInterval != 30*time.Second {
		t.Errorf("TickInterval default = %v, want 30s", cfg.TickInterval)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel default = %q, want info", cfg.LogLevel)
	}
}

func TestLoadReportsMissingRequiredVars(t *testing.T) {
	t.Setenv(config.EnvSlackAppToken, "")
	t.Setenv(config.EnvSlackBotToken, "")
	t.Setenv(config.EnvSlackUserToken, "")
	t.Setenv(config.EnvGoogleCredentials, "")
	t.Setenv(config.EnvDatabasePath, "")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for empty env, got nil")
	}
	msg := err.Error()
	for _, fragment := range []string{
		config.EnvSlackAppToken,
		config.EnvSlackBotToken,
		config.EnvSlackUserToken,
		config.EnvGoogleCredentials,
		config.EnvDatabasePath,
	} {
		if !strings.Contains(msg, fragment) {
			t.Errorf("error message missing mention of %s: %q", fragment, msg)
		}
	}
}

func TestLoadRejectsWrongTokenPrefixes(t *testing.T) {
	setValidEnv(t)
	t.Setenv(config.EnvSlackAppToken, "xoxb-wrong")
	t.Setenv(config.EnvSlackBotToken, "xapp-wrong")
	t.Setenv(config.EnvSlackUserToken, "xoxb-wrong")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for wrong token prefixes, got nil")
	}
	for _, token := range []string{
		config.EnvSlackAppToken,
		config.EnvSlackBotToken,
		config.EnvSlackUserToken,
	} {
		if !strings.Contains(err.Error(), token) {
			t.Errorf("error missing %s complaint: %v", token, err)
		}
	}
}

func TestLoadRejectsTickIntervalBelowMinimum(t *testing.T) {
	setValidEnv(t)
	t.Setenv(config.EnvTickInterval, "500ms")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for sub-second tick interval, got nil")
	}
	if !strings.Contains(err.Error(), "at least 1s") {
		t.Errorf("error should mention minimum, got: %v", err)
	}
}

func TestLoadRejectsMalformedTickInterval(t *testing.T) {
	setValidEnv(t)
	t.Setenv(config.EnvTickInterval, "not-a-duration")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for malformed tick interval, got nil")
	}
	if !strings.Contains(err.Error(), "not a valid Go duration") {
		t.Errorf("error should describe parse failure, got: %v", err)
	}
}

func TestLoadAcceptsOverriddenDefaults(t *testing.T) {
	setValidEnv(t)
	t.Setenv(config.EnvGoogleCalendarID, "team@example.com")
	t.Setenv(config.EnvTickInterval, "45s")
	t.Setenv(config.EnvLogLevel, "debug")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GoogleCalendarID != "team@example.com" {
		t.Errorf("GoogleCalendarID = %q", cfg.GoogleCalendarID)
	}
	if cfg.TickInterval != 45*time.Second {
		t.Errorf("TickInterval = %v", cfg.TickInterval)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
}
