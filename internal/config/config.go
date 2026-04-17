// Package config parses runtime configuration from environment variables,
// enforces fail-fast validation, and returns a typed Config for the rest
// of the service.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Environment variable names read by Load. Exported so tests and tools can
// reference them without duplicating string literals.
const (
	EnvSlackAppToken     = "SLACK_APP_TOKEN"
	EnvSlackBotToken     = "SLACK_BOT_TOKEN"
	EnvSlackUserToken    = "SLACK_USER_TOKEN"
	EnvSlackOwnerUserID  = "SLACK_OWNER_USER_ID"
	EnvGoogleCredentials = "GOOGLE_CALENDAR_CREDENTIALS_JSON"
	EnvGoogleCalendarID  = "GOOGLE_CALENDAR_ID"
	EnvTickInterval      = "TICK_INTERVAL"
	EnvDatabasePath      = "DATABASE_PATH"
	EnvLogLevel          = "LOG_LEVEL"
)

const (
	defaultCalendarID   = "primary"
	defaultTickInterval = 30 * time.Second
	minTickInterval     = time.Second
	defaultLogLevel     = "info"
)

// Config holds all validated runtime configuration for the presence service.
type Config struct {
	SlackAppToken         string
	SlackBotToken         string
	SlackUserToken        string
	SlackOwnerUserID      string
	GoogleCredentialsJSON string
	GoogleCalendarID      string
	TickInterval          time.Duration
	DatabasePath          string
	LogLevel              string
}

// Load reads configuration from the process environment, validates it, and
// returns either a ready-to-use Config or a single error that aggregates
// every validation failure so the operator can fix them all at once.
func Load() (*Config, error) {
	var violations []string

	appToken := os.Getenv(EnvSlackAppToken)
	if !strings.HasPrefix(appToken, "xapp-") {
		violations = append(violations, fmt.Sprintf("%s must be set and start with 'xapp-' (Slack app-level token for Socket Mode)", EnvSlackAppToken))
	}

	botToken := os.Getenv(EnvSlackBotToken)
	if !strings.HasPrefix(botToken, "xoxb-") {
		violations = append(violations, fmt.Sprintf("%s must be set and start with 'xoxb-' (Slack bot token for event/command handling)", EnvSlackBotToken))
	}

	userToken := os.Getenv(EnvSlackUserToken)
	if !strings.HasPrefix(userToken, "xoxp-") {
		violations = append(violations, fmt.Sprintf("%s must be set and start with 'xoxp-' (Slack user token for profile status/presence/DND updates)", EnvSlackUserToken))
	}

	ownerID := strings.TrimSpace(os.Getenv(EnvSlackOwnerUserID))
	if !strings.HasPrefix(ownerID, "U") || len(ownerID) < 9 {
		violations = append(violations, fmt.Sprintf("%s must be set to your Slack user ID (starts with 'U', e.g. 'U0123ABCD') — gates slash commands, App Home and modals so workspace members other than the owner cannot drive the service", EnvSlackOwnerUserID))
	}

	credentials := os.Getenv(EnvGoogleCredentials)
	if credentials == "" {
		violations = append(violations, fmt.Sprintf("%s must be set (raw Google service-account JSON as a single-line string)", EnvGoogleCredentials))
	}

	calendarID := os.Getenv(EnvGoogleCalendarID)
	if calendarID == "" {
		calendarID = defaultCalendarID
	}

	tickInterval, err := parseTickInterval(os.Getenv(EnvTickInterval))
	if err != nil {
		violations = append(violations, err.Error())
	}

	databasePath := os.Getenv(EnvDatabasePath)
	if databasePath == "" {
		violations = append(violations, fmt.Sprintf("%s must be set (absolute path to the SQLite database file)", EnvDatabasePath))
	}

	logLevel := os.Getenv(EnvLogLevel)
	if logLevel == "" {
		logLevel = defaultLogLevel
	}

	if len(violations) > 0 {
		return nil, fmt.Errorf("invalid configuration: %w", errors.New(strings.Join(violations, "; ")))
	}

	return &Config{
		SlackAppToken:         appToken,
		SlackBotToken:         botToken,
		SlackUserToken:        userToken,
		SlackOwnerUserID:      ownerID,
		GoogleCredentialsJSON: credentials,
		GoogleCalendarID:      calendarID,
		TickInterval:          tickInterval,
		DatabasePath:          databasePath,
		LogLevel:              logLevel,
	}, nil
}

func parseTickInterval(raw string) (time.Duration, error) {
	if raw == "" {
		return defaultTickInterval, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q is not a valid Go duration: %w", EnvTickInterval, raw, err)
	}
	if parsed < minTickInterval {
		return 0, fmt.Errorf("%s=%q must be at least %s to respect Slack rate limits", EnvTickInterval, raw, minTickInterval)
	}
	return parsed, nil
}
