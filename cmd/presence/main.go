// Package main is the entry point for the presence service: it loads
// configuration, opens storage, builds the Slack and Calendar clients, and
// runs the tick loop and Socket Mode event loop concurrently until
// SIGINT/SIGTERM triggers a graceful shutdown.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/muszkin/slack-presence-automation/internal/applier"
	"github.com/muszkin/slack-presence-automation/internal/calendar"
	"github.com/muszkin/slack-presence-automation/internal/config"
	"github.com/muszkin/slack-presence-automation/internal/resolver"
	slackpkg "github.com/muszkin/slack-presence-automation/internal/slack"
	"github.com/muszkin/slack-presence-automation/internal/storage"
	"github.com/muszkin/slack-presence-automation/internal/ui"
)

func main() {
	if err := run(); err != nil {
		slog.Error("presence service failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.InfoContext(rootCtx, "presence service starting",
		slog.String("tick_interval", cfg.TickInterval.String()),
		slog.String("calendar_id", cfg.GoogleCalendarID),
		slog.String("database_path", cfg.DatabasePath))

	store, err := storage.Open(rootCtx, cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.ErrorContext(rootCtx, "close storage", slog.Any("err", err))
		}
	}()

	calClient, err := calendar.NewClient(rootCtx, cfg.GoogleCredentialsJSON, cfg.GoogleCalendarID)
	if err != nil {
		return fmt.Errorf("build calendar client: %w", err)
	}

	statusClient := slackpkg.NewStatusClient(cfg.SlackUserToken, logger)
	state := applier.New(statusClient, store, logger)

	trigger := make(chan struct{}, 1)
	socket := slackpkg.NewSocketRunner(cfg.SlackAppToken, cfg.SlackBotToken, logger)
	views := slackpkg.NewViewsClient(socket.Client())
	commands := ui.NewCommands(store, views, trigger, logger)

	socket.SetSlashHandler(commands.Handle)
	socket.SetHomeOpenedHandler(commands.HandleHomeOpened)
	socket.SetInteractionHandler(commands.HandleInteraction)

	g, gctx := errgroup.WithContext(rootCtx)
	g.Go(func() error {
		return runTickLoop(gctx, state, store, calClient, cfg.TickInterval, trigger, logger)
	})
	g.Go(func() error {
		return socket.Run(gctx)
	})

	logger.InfoContext(rootCtx, "presence service ready")
	err = g.Wait()
	logger.InfoContext(rootCtx, "presence service shutting down")
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func runTickLoop(
	ctx context.Context,
	appl *applier.Applier,
	store *storage.Store,
	cal *calendar.Client,
	interval time.Duration,
	trigger <-chan struct{},
	logger *slog.Logger,
) error {
	reconcile := func() {
		if err := reconcileOnce(ctx, appl, store, cal, logger); err != nil {
			logger.WarnContext(ctx, "reconcile failed", slog.Any("err", err))
		}
	}
	reconcile()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			reconcile()
		case <-trigger:
			reconcile()
		}
	}
}

func reconcileOnce(
	ctx context.Context,
	appl *applier.Applier,
	store *storage.Store,
	cal *calendar.Client,
	logger *slog.Logger,
) error {
	now := time.Now()
	nowUnix := now.Unix()

	if _, err := store.DeleteExpiredOverrides(ctx, nowUnix); err != nil {
		logger.WarnContext(ctx, "cleanup expired overrides", slog.Any("err", err))
	}

	overrideRows, err := store.ListActiveOverrides(ctx, nowUnix)
	if err != nil {
		return fmt.Errorf("list active overrides: %w", err)
	}
	ruleRows, err := store.ListScheduleRules(ctx)
	if err != nil {
		return fmt.Errorf("list schedule rules: %w", err)
	}
	patternRows, err := store.ListMeetingPatterns(ctx)
	if err != nil {
		return fmt.Errorf("list meeting patterns: %w", err)
	}

	dayStart := startOfLocalDay(now)
	dayEnd := dayStart.AddDate(0, 0, 1)
	events, err := cal.FetchEvents(ctx, dayStart, dayEnd)
	if err != nil {
		logger.WarnContext(ctx, "fetch calendar events, continuing with empty set",
			slog.Any("err", err))
		events = nil
	}

	active := activeEventCount(events, now)
	logger.InfoContext(ctx, "reconcile tick",
		slog.Int("overrides", len(overrideRows)),
		slog.Int("rules", len(ruleRows)),
		slog.Int("patterns", len(patternRows)),
		slog.Int("events_today", len(events)),
		slog.Int("events_active_now", active))

	if len(events) > 0 && active == 0 {
		logger.InfoContext(ctx, "fetched events today but none active at this moment",
			slog.String("now", now.Format(time.RFC3339)),
			slog.Any("titles", eventTitles(events)))
	}

	desired := resolver.Resolve(now,
		toResolverOverrides(overrideRows),
		toResolverEvents(events),
		toResolverRules(ruleRows),
		toResolverPatterns(patternRows),
	)
	logger.InfoContext(ctx, "resolved desired state",
		slog.String("source", string(desired.Source)),
		slog.String("presence", string(desired.Presence)),
		slog.String("emoji", desired.StatusEmoji),
		slog.String("text", desired.StatusText))
	return appl.Apply(ctx, desired)
}

func activeEventCount(events []calendar.Event, now time.Time) int {
	n := 0
	for _, e := range events {
		if !now.Before(e.Start) && now.Before(e.End) {
			n++
		}
	}
	return n
}

func eventTitles(events []calendar.Event) []string {
	titles := make([]string, 0, len(events))
	for _, e := range events {
		titles = append(titles, fmt.Sprintf("%s [%s..%s]",
			e.Title, e.Start.Format(time.RFC3339), e.End.Format(time.RFC3339)))
	}
	return titles
}

func toResolverPatterns(rows []storage.MeetingPattern) []resolver.MeetingPattern {
	out := make([]resolver.MeetingPattern, 0, len(rows))
	for _, r := range rows {
		out = append(out, resolver.MeetingPattern{
			ID:           r.ID,
			TitlePattern: r.TitlePattern,
			StatusEmoji:  r.StatusEmoji,
			StatusText:   r.StatusText,
			Presence:     resolver.Presence(r.Presence),
			Priority:     int(r.Priority),
		})
	}
	return out
}

func toResolverOverrides(rows []storage.Override) []resolver.Override {
	out := make([]resolver.Override, 0, len(rows))
	for _, r := range rows {
		out = append(out, resolver.Override{
			ExpiresAt:   time.Unix(r.ExpiresAt, 0),
			StatusEmoji: r.StatusEmoji,
			StatusText:  r.StatusText,
			Presence:    resolver.Presence(r.Presence),
		})
	}
	return out
}

func toResolverEvents(events []calendar.Event) []resolver.Event {
	out := make([]resolver.Event, 0, len(events))
	for _, e := range events {
		out = append(out, resolver.Event{
			Title: e.Title,
			Start: e.Start,
			End:   e.End,
		})
	}
	return out
}

func toResolverRules(rows []storage.ScheduleRule) []resolver.Rule {
	out := make([]resolver.Rule, 0, len(rows))
	for _, r := range rows {
		out = append(out, resolver.Rule{
			ID:          r.ID,
			DaysOfWeek:  uint8(r.DaysOfWeek),
			StartMinute: int(r.StartMinute),
			EndMinute:   int(r.EndMinute),
			StatusEmoji: r.StatusEmoji,
			StatusText:  r.StatusText,
			Presence:    resolver.Presence(r.Presence),
			Priority:    int(r.Priority),
		})
	}
	return out
}

func startOfLocalDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
