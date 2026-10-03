# Noccounting

A travel expense tracker for Telegram, backed by your own self-hosted [TREK](https://github.com/mauriceboe/TREK) instance.

Log expenses from a bot or a Mini App, scan receipts with an LLM, and see who owes whom. Every expense lives in a TREK trip, so there is no second database.

## Install

Requires Go 1.27+, [`templ`](https://templ.guide), [`tsgo`](https://github.com/microsoft/typescript-go), the [Tailwind CSS v4 standalone CLI](https://tailwindcss.com/blog/standalone-cli) and [`task`](https://taskfile.dev).

```sh
git clone https://github.com/omegaatt36/noccounting
cd noccounting
task build
```

Or with Docker (amd64 / arm64):

```sh
docker run -d -p 8080:8080 \
  -e TELEGRAM_BOT_TOKEN -e TREK_URL -e TREK_EMAIL -e TREK_PASSWORD -e USER_MAPPING \
  omegaatt36/noccounting
```

## Quick Start

Copy `.env.example` to `.env`, fill it in, then:

```sh
task run
```

- Add the bot's TREK service account to a trip, then send `/trip` to pick it.
- Send `/quick` to enter an expense step by step, or send a receipt photo.
- Send `/summary` to see who owes whom.

## Commands

| Command | Action |
|---|---|
| `/add <name> <price> <currency> <category> <method> [date]` | Quick add |
| `/quick` | Step-by-step entry |
| `/list [method]` | Latest expenses |
| `/summary` | Balances and transfers |
| `/today` | Today by category |
| `/rate` | Exchange rates |
| `/edit` | Edit recent expenses |
| `/trip` | Switch trip |
| *photo* | Scan a receipt |

Currencies are TWD and JPY, with rates from [FinMind](https://finmindtrade.com/). Categories and payment methods follow TREK's own sets.

## Configuration

Environment variables only:

```sh
TELEGRAM_BOT_TOKEN=...
TREK_URL=https://trek.example.com   # https, no trailing slash
TREK_EMAIL=bot@example.com          # dedicated account, no 2FA
TREK_PASSWORD=...

# optional
USER_MAPPING=telegram_id:trek_user_id:nickname,...
WEBAPP_URL=https://your-webapp-url.com
PORT=8080
POLLER=long                         # or "webhook", which also needs the three below
WEBHOOK_PUBLIC_URL=...              # https URL Telegram posts to, with a random path
WEBHOOK_SECRET_TOKEN=...            # A-Za-z0-9_-, checked on every request
WEBHOOK_LISTEN=:8081                # where the webhook server binds
DEV_MODE=false                      # skips Telegram auth, local only
LOG_LEVEL=INFO
LLM_API_KEY=...                     # OpenAI-compatible vision API, for receipts
LLM_BASE_URL=...
LLM_MODEL=gpt-4o
```

The bot logs in as its own TREK account and sees only the trips it is added to. Add it when a trip starts and remove it when it ends.

## License

[MIT](LICENSE)
