# slack-presence-automation

Private, single-user service that keeps your Slack profile status, presence, and Do-Not-Disturb in sync with your Google Calendar and a set of custom schedule rules. Runs on a home server without requiring a public IP (Slack Socket Mode, outbound WebSocket only).

## Features

- **Automatic status from calendar events** — an active meeting becomes `:calendar: In a meeting` + DND by default; customisable per event-title pattern.
- **Manual overrides via `/presence`** — `/presence focus 2h`, `/presence away 30m`, `/presence dnd 1h`, `/presence available 8h`, `/presence clear`.
- **Schedule rules** — recurring patterns like "Mon–Fri 09:00–10:00 deep work" managed from the App Home tab with Block Kit modals.
- **Meeting patterns** — map event-title substrings to custom emoji/presence (e.g. "Lunch" → away + `:hamburger:`).
- **Deterministic priority resolver** — `override > calendar > schedule > default`; every decision is a pure-function output logged with its source.
- **App Home tab** — read current applied state, active overrides, rules, patterns; add and delete via modals.
- **Apply-on-change** — `applied_state` cache in SQLite dedups writes so the Slack rate budget is respected.

## Stack

Go 1.25 · SQLite (`modernc.org/sqlite` + `sqlc` + `goose`) · `slack-go/slack` (Socket Mode) · Google Calendar API (service account) · Docker Compose · `golangci-lint`.

## Getting started

1. Follow [`SETUP.md`](SETUP.md) — step-by-step guide to create the Slack app from the bundled manifest, provision a Google service account, share your calendar with it, and populate `.env`.
2. Pull the pre-built image from GHCR or build locally:

   ```sh
   # from GHCR (after CI publishes it)
   docker pull ghcr.io/muszkin/slack-presence-automation:latest

   # or build locally
   docker compose up -d --build
   ```

3. Watch the logs:

   ```sh
   docker compose logs -f presence
   ```

   You should see `reconcile tick` entries every 30s with event counts and the resolved desired state.

## Configuration

Configuration is 12-factor: environment variables only, documented in [`.env.example`](.env.example). Three Slack tokens are required:

- `SLACK_APP_TOKEN` (xapp-…) — Socket Mode
- `SLACK_BOT_TOKEN` (xoxb-…) — slash commands, App Home
- `SLACK_USER_TOKEN` (xoxp-…) — sets the human user's profile status, presence, and DND (bot tokens cannot)

Google Calendar uses a service-account JSON credential. **Important**: set `GOOGLE_CALENDAR_ID` to your email address — `primary` resolves to the service account's own empty calendar, not yours.

## Development

```sh
go test ./...
golangci-lint run
sqlc generate   # regenerate queries after editing internal/storage/queries/*.sql
```

All migrations live in `internal/storage/migrations/` and are applied at service startup by goose.

## Architecture

- `cmd/presence` — entry point, tick loop, Socket Mode wiring
- `internal/config` — env parsing + fail-fast validation
- `internal/storage` — SQLite, migrations, sqlc-generated queries
- `internal/resolver` — pure-function `Resolve(now, overrides, events, rules, patterns) -> DesiredState`
- `internal/calendar` — Google Calendar client (service-account JWT)
- `internal/slack` — Socket Mode runner, user-token status client, views.publish / views.open wrappers
- `internal/applier` — reconciles desired state vs `applied_state`, pushes changes to Slack
- `internal/ui` — `/presence` slash commands, App Home view builder, Block Kit modals, interaction dispatcher

## Deployment

CI (`.github/workflows/docker.yml`) builds a multi-arch (amd64 + arm64) image on every push to `main` and publishes it to `ghcr.io/muszkin/slack-presence-automation`. Pin a specific tag in `docker-compose.yml` for production.

## License

Private project. No license granted.
