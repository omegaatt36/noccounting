# Noccounting

A travel expense tracker with Telegram Bot and Web Mini App interfaces, using Notion as the backend database.

## Features

- **Multi-currency** - Track expenses in TWD and JPY with real-time exchange rate conversion (FinMind API)
- **6 Categories** - 🍜 食 · 🏠 住 · 🚃 行 · 🛍️ 購 · 🎯 樂 · 📎 雜
- **Receipt Scanning** - Send a receipt photo to the bot; LLM vision extracts items, prices, and categories automatically
- **Dual Interface** - Telegram Bot for quick entries, Web Mini App for richer form input
- **Notion as DB** - All data stored in Notion for easy viewing and collaboration
- **Smart Defaults** - The Mini App remembers your last currency, category, and payment method via localStorage

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.26 |
| Bot | [telebot.v4](https://gopkg.in/telebot.v4) |
| Web Frontend | [Templ](https://templ.guide) + [HTMX](https://htmx.org) + [Tailwind CSS v4](https://tailwindcss.com) |
| TypeScript | [tsgo](https://github.com/microsoft/typescript-go) (Go-native compiler, zero node_modules) |
| UI Components | [templui](https://templui.com) |
| Theme | [Flexoki Dark](https://stephango.com/flexoki) palette |
| Database | Notion API |
| Receipt Analysis | OpenAI-compatible vision API (via LiteLLM or similar) |
| Exchange Rates | [FinMind API](https://finmindtrade.com/) |
| Deployment | Docker (multi-arch amd64/arm64) |
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
USER_MAPPING=telegram_id1:notion_id1:nickname1,telegram_id2:notion_id2:nickname2

# Optional
WEBAPP_URL=https://your-webapp-url.com   # Mini App URL for bot menu button
PORT=8080                                 # Web server port (webapp only)
LOG_LEVEL=INFO                            # DEBUG, INFO, WARN, ERROR
SQLITE_PATH=./noccounting.db              # Local SQLite ledger database path

# Legacy seed (optional)
NOTION_DATABASE_ID=your_notion_database_id  # Used only for initial seeding on first boot

# Receipt scanning (optional)
LLM_API_KEY=your_api_key
LLM_BASE_URL=https://your-llm-endpoint/  # OpenAI-compatible endpoint
LLM_MODEL=your_model_name                # e.g. gpt-4o
```

### Build & Run

The binary reads configuration exclusively from environment variables.
Copy `.env.example` to `.env` and fill in values — `godotenv` loads it automatically at startup.

```bash
# Generate all (templ + tsgo + tailwind)
task generate

# Run tests
task test

# Build binary
task build

# Run (.env loaded automatically)
./bin/noccounting

# Run in dev mode (skips Telegram auth)
DEV_MODE=true ./bin/noccounting
```

### Taskfile Commands

| Command | Description |
|---------|-------------|
| `task generate` | Run all code generation (templ + tsgo + tailwind) |
| `task ts` | Compile TypeScript only |
| `task css` | Build CSS only |
| `task css:watch` | Watch CSS for changes |
| `task test` | Run all tests with race detector |
| `task build` | Generate + build the binary |
| `task lint` | Run `go vet` |

## Notion Database Setup

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
| `/ledgers` | List all registered ledgers |
| `/ledger_add` | Register a new ledger: `/ledger_add <name> <notion_db_id>` |
| `/ledger_use` | Switch active ledger: `/ledger_use <name>` |
| *Photo* | Send a receipt photo for automatic LLM analysis |

## Web API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/` | Serves the Mini App |
| GET | `/api/auth` | Validates Telegram WebApp initData |
| GET | `/api/users` | Lists authorized users |
| POST | `/api/expense` | Creates a new expense |
| GET | `/api/export/csv` | Export expenses as CSV |
| GET | `/health` | Health check |

## Docker

```bash
# Build
docker build -f build/noccounting/Dockerfile -t noccounting .

# Run
docker run -d -p 8080:8080 \
  -v noccounting-data:/data \
  -e SQLITE_PATH=/data/noccounting.db \
  -e TELEGRAM_BOT_TOKEN -e NOTION_TOKEN -e USER_MAPPING \
  noccounting
```

Images are published to Docker Hub on git tag push (`v*`):
- `omegaatt36/noccounting`

The image supports `linux/amd64` and `linux/arm64`.

## License

[MIT](LICENSE)
