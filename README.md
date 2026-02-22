# Noccounting

A travel expense tracker with Telegram Bot and Web Mini App interfaces, using Notion as the backend database.

## Features

- **Multi-currency** - Track expenses in TWD and JPY with real-time exchange rate conversion (FinMind API)
- **6 Categories** - 🍜 食 · 🏠 住 · 🚃 行 · 🛍️ 購 · 🎯 樂 · 📎 雜
- **Receipt Scanning** - Send a receipt photo to the bot; LLM vision extracts items, prices, and categories automatically
- **Dual Interface** - Telegram Bot for quick entries, Web Mini App for richer form input
- **Notion as DB** - All data stored in Notion for easy viewing and collaboration
- **Smart Defaults** - The Mini App remembers your last currency, category, and payment method via localStorage

## Architecture

```
cmd/
├── bot/            Telegram Bot binary
└── webapp/         Web Mini App binary (HTTP server)

domain/             Business models, enums, repository interfaces

internal/
├── app/
│   ├── bot/        Telegram handlers (text commands, receipt photo, inline callbacks)
│   └── webapp/     HTTP handlers, Templ components, middleware, static serving
├── infrastructure/
│   ├── llm/        OpenAI-compatible vision API client (receipt analysis)
│   └── exchangerate/  FinMind exchange rate client
├── persistence/
│   ├── notion/     Notion API client (expenses CRUD + file upload)
│   └── user/       In-memory user repository (from USER_MAPPING)
└── service/        Business service layer

web/ts/             TypeScript source (compiled to JS via tsgo)
build/              Dockerfiles (bot, webapp)
helm/               Kubernetes Helm chart
```

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.26 |
| Bot | [telebot.v4](https://gopkg.in/telebot.v4) |
| Web Frontend | [Templ](https://templ.guide) components + [HTMX](https://htmx.org) + [Tailwind CSS v4](https://tailwindcss.com) |
| TypeScript | [tsgo](https://github.com/microsoft/typescript-go) (Go-native compiler, zero node_modules) |
| Theme | Nord dark palette |
| Database | Notion API |
| Receipt Analysis | OpenAI-compatible vision API (via LiteLLM or similar) |
| Exchange Rates | [FinMind API](https://finmindtrade.com/) |
| Deployment | Docker (multi-arch amd64/arm64), Kubernetes (Helm) |
| Task Runner | [Taskfile](https://taskfile.dev) |

## Getting Started

### Prerequisites

- Go 1.26+
- [tsgo](https://github.com/microsoft/typescript-go): `go install github.com/microsoft/typescript-go/cmd/tsgo@latest`
- [templ](https://templ.guide): `go install github.com/a-h/templ/cmd/templ@latest`
- [Tailwind CSS v4 standalone CLI](https://tailwindcss.com/blog/standalone-cli)
- [Taskfile](https://taskfile.dev): `go install github.com/go-task/task/v3/cmd/task@latest`

### Environment Variables

```bash
# Required
TELEGRAM_BOT_TOKEN=your_telegram_bot_token
NOTION_TOKEN=your_notion_integration_token
NOTION_DATABASE_ID=your_notion_database_id
USER_MAPPING=telegram_id1:notion_id1:nickname1,telegram_id2:notion_id2:nickname2

# Optional
WEBAPP_URL=https://your-webapp-url.com   # Mini App URL for bot menu button
PORT=8080                                 # Web server port (webapp only)
LOG_LEVEL=INFO                            # DEBUG, INFO, WARN, ERROR

# Receipt scanning (optional)
LLM_API_KEY=your_api_key
LLM_BASE_URL=https://your-llm-endpoint/  # OpenAI-compatible endpoint
LLM_MODEL=your_model_name                # e.g. gpt-5
```

### Build & Run

```bash
# Generate all (templ + tsgo + tailwind)
task generate

# Run tests
task test

# Build binaries
task build

# Run bot
./bin/bot \
  --telegram-token=$TELEGRAM_BOT_TOKEN \
  --notion-token=$NOTION_TOKEN \
  --notion-database-id=$NOTION_DATABASE_ID \
  --user-mapping=$USER_MAPPING

# Run webapp (with Telegram auth)
./bin/webapp \
  --telegram-bot-token=$TELEGRAM_BOT_TOKEN \
  --notion-token=$NOTION_TOKEN \
  --notion-database-id=$NOTION_DATABASE_ID \
  --user-mapping=$USER_MAPPING \
  --port=8080

# Run webapp (dev mode — skips Telegram auth)
./bin/webapp --dev-mode \
  --notion-token=$NOTION_TOKEN \
  --notion-database-id=$NOTION_DATABASE_ID \
  --user-mapping=$USER_MAPPING \
  --port=8080
```

### Taskfile Commands

| Command | Description |
|---------|-------------|
| `task generate` | Run all code generation (templ + tsgo + tailwind) |
| `task ts` | Compile TypeScript only |
| `task css` | Build CSS only |
| `task css:watch` | Watch CSS for changes |
| `task test` | Run all tests with race detector |
| `task build` | Generate + build both binaries |
| `task lint` | Run `go vet` |

## Notion Database Setup

Create a Notion database with these properties:

| Property | Type | Values |
|----------|------|--------|
| name | Title | Expense name |
| price | Number | Amount in source currency |
| currency | Select | `TWD`, `JPY` |
| category | Select | `食`, `住`, `行`, `購`, `樂`, `雜` |
| method | Select | `cash`, `credit_card`, `ic_card`, `e_pay` |
| shopped_at | Date | Transaction date |
| paid_by | People | Notion user who paid |
| ex_rate | Number | Exchange rate (JPY expenses) |

## Bot Commands

| Command | Description |
|---------|-------------|
| `/start` | Welcome message with Mini App button |
| `/help` | Show available commands |
| `/add` | Quick add: `/add <name> <price> <currency> <category> <method>` |
| `/quick` | Interactive step-by-step expense entry |
| `/list` | Show all expenses |
| `/summary` | Expense summary grouped by payer |
| `/today` | Today's expenses grouped by category |
| `/edit` | Modify recent expenses |
| `/cancel` | Cancel current conversation |
| *Photo* | Send a receipt photo for automatic LLM analysis |

## Web API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/` | Serves the Mini App (Templ-rendered) |
| GET | `/api/auth` | Validates Telegram WebApp initData |
| GET | `/api/users` | Lists authorized users |
| POST | `/api/expense` | Creates a new expense (HTMX) |
| GET | `/health` | Health check |
| GET | `/static/*` | Static assets (CSS, JS) |

## Docker

```bash
# Build
docker build -f build/bot/Dockerfile -t noccounting-bot .
docker build -f build/webapp/Dockerfile -t noccounting-web .

# Run
docker run -d \
  -e TELEGRAM_BOT_TOKEN -e NOTION_TOKEN -e NOTION_DATABASE_ID -e USER_MAPPING \
  noccounting-bot

docker run -d -p 8080:8080 \
  -e TELEGRAM_BOT_TOKEN -e NOTION_TOKEN -e NOTION_DATABASE_ID -e USER_MAPPING \
  noccounting-web
```

Images are published to Docker Hub on git tag push (`v*`):
- `omegaatt36/noccounting-bot`
- `omegaatt36/noccounting-web`

Both images support `linux/amd64` and `linux/arm64`.

## Helm

```bash
helm install noccounting ./helm \
  --set bot.telegramToken=$TELEGRAM_BOT_TOKEN \
  --set notion.token=$NOTION_TOKEN \
  --set notion.databaseId=$NOTION_DATABASE_ID \
  --set userMapping=$USER_MAPPING
```

## Frontend Architecture

The Mini App frontend uses a **Go-only toolchain** with zero `node_modules`:

```
web/ts/*.ts  →  tsgo compile  →  static/*.js  →  go:embed  →  single binary
                                  (ES Modules)
```

| Module | Responsibility |
|--------|---------------|
| `app.ts` | Entry point, initialization orchestration |
| `telegram.ts` | Telegram WebApp SDK wrapper, haptic feedback |
| `auth.ts` | Authentication flow, user loading |
| `form.ts` | Button groups, HTMX form lifecycle, auto-dismiss |
| `exchange-rate.ts` | JPY rate fetching (FinMind API) + localStorage cache |
| `storage.ts` | Smart defaults via localStorage |

## License

[MIT](LICENSE)
