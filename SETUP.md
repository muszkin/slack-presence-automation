# SETUP

Przewodnik krok po kroku: jak utworzyć aplikację Slack, skonfigurować Google Calendar API i zebrać wszystkie wartości do pliku `.env`.

Efekt końcowy: uruchomiony `docker compose up -d` na home serwerze, automatycznie aktualizujący status i presence w Slacku.

---

## 1. Slack — utworzenie aplikacji z manifestu

Manifest aplikacji leży w repo: `slack-app-manifest.yaml`. Zawiera wszystkie wymagane scope'y, Socket Mode, slash command `/presence` oraz App Home.

### 1.1. Utwórz aplikację z manifestu

1. Otwórz https://api.slack.com/apps i kliknij **Create New App** → **From a manifest**.
2. Wybierz workspace, w którym aplikacja ma działać (Twój prywatny workspace).
3. W kroku "Enter app manifest below" przełącz na zakładkę **YAML** i wklej całą zawartość `slack-app-manifest.yaml`.
4. Kliknij **Next** → **Create**.

### 1.2. App-level token (xapp-) → `SLACK_APP_TOKEN`

Potrzebny do Socket Mode.

1. W ustawieniach aplikacji przejdź do **Basic Information** → sekcja **App-Level Tokens** → **Generate Token and Scopes**.
2. Nazwa: `socket-mode`. Dodaj scope **`connections:write`**.
3. Kliknij **Generate** i skopiuj token (zaczyna się od `xapp-`). Zapisz go jako `SLACK_APP_TOKEN`.

### 1.3. Bot token (xoxb-) → `SLACK_BOT_TOKEN`

Potrzebny do obsługi slash commandów, App Home i eventów.

1. Przejdź do **Install App** w menu po lewej.
2. Kliknij **Install to Workspace** i zatwierdź uprawnienia.
3. Po instalacji na tej samej stronie pojawi się **Bot User OAuth Token** (zaczyna się od `xoxb-`). Skopiuj go jako `SLACK_BOT_TOKEN`.

### 1.4. User token (xoxp-) → `SLACK_USER_TOKEN`

**Najważniejszy krok.** Bot token NIE MA uprawnień do zmiany statusu/presence/DND człowieka — do tego potrzebny jest token użytkownika (xoxp-).

1. Na stronie **Install App** (ta sama co powyżej) poniżej Bot Token znajduje się **User OAuth Token** (zaczyna się od `xoxp-`). Skopiuj go jako `SLACK_USER_TOKEN`.

Jeśli go nie widzisz, upewnij się że manifest był użyty w całości — user scopes (`users.profile:write`, `users:write`, `dnd:write`, itp.) muszą być w `oauth_config.scopes.user`. Przy ich braku reinstaluj aplikację: **Install App** → **Reinstall to Workspace**.

### 1.5. Twój Slack User ID → `SLACK_OWNER_USER_ID`

Aplikacja zainstalowana w workspace staje się *widoczna dla wszystkich* członków workspace'u — każdy mógłby wpisać `/presence` i sterować Twoim statusem. Dlatego serwis wymaga `SLACK_OWNER_USER_ID` i odrzuca komendy/interakcje od innych osób.

Jak zdobyć swój User ID:

1. Slack desktop/web → klikasz swoje zdjęcie w prawym górnym rogu → **Profile**.
2. W prawym górnym rogu profilu: **...** (More) → **Copy member ID**.
3. Skopiowany ciąg zaczyna się od `U` (np. `U0123ABCD`). To wartość `SLACK_OWNER_USER_ID` w `.env`.

Alternatywnie: w Slacku w dowolnym channelu napisz `/apps` lub kliknij swoje zdjęcie → **Profile** — URL profilu zawiera ID po `/team/`.

Skutek:
- inni członkowie workspace'u widzą slash command `/presence` ale dostają ephemeral notę "This presence app is private to its owner"
- otwarcie App Home przez nie-ownera renderuje krótki komunikat o prywatności
- submity modali od nie-ownerów są odrzucane

### 1.6. Signing Secret (opcjonalnie)

Socket Mode nie wymaga walidacji signatur HTTP, więc `SLACK_SIGNING_SECRET` nie jest używany w tej aplikacji. Pomijamy.

---

## 2. Google Calendar — service account + udostępnienie kalendarza

Używamy service account (uproszczenie dla single-user; nie wymaga interaktywnego OAuth flow, dobry wybór dla długo działającego home serwera).

### 2.1. Utwórz projekt w Google Cloud

1. Wejdź na https://console.cloud.google.com/ i zaloguj się.
2. Utwórz nowy projekt (np. "presence-home").

### 2.2. Włącz Google Calendar API

1. W projekcie przejdź do **APIs & Services** → **Library**.
2. Wyszukaj "Google Calendar API" i kliknij **Enable**.

### 2.3. Utwórz service account

1. **APIs & Services** → **Credentials** → **Create Credentials** → **Service account**.
2. Nazwa: `presence-reader`. Opis: "Read-only access to personal calendar".
3. W kroku uprawnień pomiń (zostaw puste — nie potrzebujemy ról GCP).
4. Pomiń krok "Grant users access".
5. Kliknij **Done**.

### 2.4. Wygeneruj klucz JSON → `GOOGLE_CALENDAR_CREDENTIALS_JSON`

1. Na liście service accountów kliknij utworzony `presence-reader`.
2. Zakładka **Keys** → **Add Key** → **Create new key** → wybierz **JSON** → **Create**.
3. Google pobierze plik `.json`. **Otwórz go, skopiuj całą zawartość i wklej jako jedną linię** do `GOOGLE_CALENDAR_CREDENTIALS_JSON` w pliku `.env`.

   Alternatywnie można użyć `jq -c . credentials.json` żeby zminifikować do jednej linii.

### 2.5. Udostępnij swój kalendarz service accountowi

Service account jest osobnym "użytkownikiem" Google — bez jawnego share'a nie widzi Twojego kalendarza.

1. Na liście kluczy/detali service accountu skopiuj jego email (postaci `presence-reader@project-id.iam.gserviceaccount.com`).
2. Otwórz Google Calendar → ustawienia kalendarza który ma być czytany (np. "My Calendar").
3. Sekcja **Share with specific people or groups** → **Add people and groups**.
4. Wklej email service accountu, ustaw uprawnienia **See all event details** (read-only).
5. **Send**.

### 2.6. `GOOGLE_CALENDAR_ID`

**WAŻNE**: dla service account auth `primary` **NIE** działa. Service account jest osobnym "użytkownikiem" Google — `primary` wskazuje na jego własny pusty kalendarz, nie na twój (udostępnienie nic nie zmienia w tej kwestii). Fetch wróci 0 eventów bez żadnego błędu.

- **Główny kalendarz użytkownika** (prawie zawsze właściwy wybór): wpisz swój **adres email Google** (np. `ty@gmail.com` albo `ty@firma.pl`). To jest też Calendar ID Twojego głównego kalendarza.
- **Inny kalendarz** (np. zespołowy): Google Calendar → ustawienia konkretnego kalendarza → sekcja **Integrate calendar** → skopiuj **Calendar ID** (formatu `xxx@group.calendar.google.com`).

Jeśli log mówi `events_today=0` mimo że masz eventy na dziś, w 99% przypadków przyczyną jest `GOOGLE_CALENDAR_ID=primary` zamiast twojego emaila.

---

## 3. `.env` — zebranie wszystkiego

1. Skopiuj szablon: `cp .env.example .env`.
2. Wypełnij wartości:

   ```
   SLACK_APP_TOKEN=xapp-1-ABC...                      # z kroku 1.2
   SLACK_BOT_TOKEN=xoxb-1-ABC...                      # z kroku 1.3
   SLACK_USER_TOKEN=xoxp-1-ABC...                     # z kroku 1.4
   GOOGLE_CALENDAR_CREDENTIALS_JSON={"type":"ser...}  # z kroku 2.4 (jedna linia!)
   GOOGLE_CALENDAR_ID=primary                         # z kroku 2.6
   TICK_INTERVAL=30s                                  # domyślnie 30s, min 1s
   DATABASE_PATH=/data/presence.db                    # ścieżka w kontenerze
   LOG_LEVEL=info                                     # debug | info | warn | error
   ```

3. Upewnij się, że `.env` jest w `.gitignore` (zweryfikuj `git check-ignore .env`) — **nigdy nie commituj tokenów**.

---

## 4. Uruchomienie

```bash
docker compose up -d --build
docker compose logs -f presence
```

Pierwszy tick powinien natychmiast po starcie:

1. wczytać konfigurację z env,
2. otworzyć bazę SQLite w volumenie,
3. zastosować migracje goose,
4. nawiązać WebSocket Socket Mode do Slacka,
5. pobrać eventy z kalendarza na dziś,
6. wywołać resolver, zastosować status w Slacku.

W logu powinny pojawić się linie JSON `presence service starting` → `slack socket mode connecting` → `slack socket mode connected` → `applying desired state` (lub brak apply jeśli nic się nie zmieniło).

---

## 5. Test szybki

W Slacku wpisz:

```
/presence
```

Powinieneś dostać ephemeral reply z aktualnym applied state i liczbą aktywnych override'ów.

Zrób manual override na 5 minut:

```
/presence focus 5m
```

Status w Twoim profilu powinien zmienić się na `:focus: focus`, DND aktywne. Po 5 minutach (plus ≤1 tick) wróci do poprzedniego stanu.

Wyczyść ręcznie:

```
/presence clear
```

---

## 6. Najczęstsze problemy

| Problem | Przyczyna / fix |
|---------|-----------------|
| `SLACK_USER_TOKEN must be set and start with 'xoxp-'` | User scopes nie ma w manifeście LUB reinstall po manifest update zatrzymał się na bot tokenie. Przejdź do **Install App** → **Reinstall to Workspace** i skopiuj nowy User Token. |
| `fetch calendar events ... HTTP 404 Not Found` | Service account nie ma dostępu do kalendarza. Krok 2.5. |
| `Not Authorized to access this resource/api` | Calendar API nie włączone. Krok 2.2. |
| `/presence` w Slacku zwraca "dispatch_failed" | Socket Mode nie jest aktywny lub manifest nie włączył `socket_mode_enabled: true`. Zweryfikuj w **Socket Mode** po lewej w ustawieniach aplikacji — musi być **On**. |
| Status nie zmienia się mimo pomyślnego reconcile | User token (xoxp-) nie ma `users.profile:write` / `users:write` / `dnd:write`. Przejrzyj sekcję **OAuth & Permissions** → **User Token Scopes**, dodaj brakujące i zrób **Reinstall**. |
| Docker image nie startuje: `database is locked` | Inny proces otwarł tę samą bazę (np. stary kontener zostawił lock). `docker compose down` + usunięcie `presence-data` volume. |
| `open storage: ping sqlite ... unable to open database file (14)` i restart loop | Volume `presence-data` został utworzony z ownership root przed poprawką Dockerfile — kontener biega jako UID 65532 i nie może pisać. Fix: `docker compose down -v && docker compose up -d --build`. Flaga `-v` usuwa stary volume; nowy zostanie zainicjalizowany z poprawnym ownership z image. |

---

## 7. Rotacja tokenów

Wszystkie tokeny można w dowolnej chwili zrotować:

- **App token**: Basic Information → App-Level Tokens → **Revoke** → wygeneruj nowy z scope `connections:write`.
- **Bot + User tokens**: Install App → **Uninstall**, potem **Reinstall**. Oba tokeny dostaniesz nowe.
- **Google service account key**: Credentials → service account → Keys → **Delete** starego + **Create new key**. Stary przestaje działać natychmiast.

Po rotacji zaktualizuj `.env` i `docker compose up -d --force-recreate presence`.
