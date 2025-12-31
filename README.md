# Noccounting

A travel expense tracker with Telegram Bot and Web Mini App interfaces, using Notion as the backend database.

## Features

- **Multi-currency Support** - Track expenses in TWD and JPY with automatic exchange rate conversion
- **Expense Categories** - Categorize by Food (食), Clothing (衣), Housing (住), Transportation (行), Entertainment (樂)
- **Payment Methods** - Record payments via cash, credit card, IC card, or PayPay
- **Dual Interface** - Use either the Telegram Bot or embedded Web Mini App
- **Notion Integration** - All data stored in a Notion database for easy viewing and collaboration
- **Real-time Exchange Rates** - Automatic JPY→TWD conversion via FinMind API

## Architecture

```
┌─────────────────────────────────────────────────┐
│ Command Layer (cmd/)                            │
│ - bot/main.go (Telegram Bot)                    │
│ - webapp/main.go (HTTP Server)                  │
└─────────────────────────┬───────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────┐
│ Application Layer (internal/app/)               │
│ - bot/         (Telegram handlers)              │
│ - webapp/      (HTTP handlers + Mini App)       │
└─────────────────────────┬───────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────┐
│ Domain Layer (domain/)                          │
│ - Models (Expense, User, Currency, Category)    │
│ - Repository interfaces                         │
└─────────────────────────┬───────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────┐
│ Infrastructure Layer                            │
│ - persistence/notion (Notion API client)        │
│ - persistence/user (User repository)            │
│ - infrastructure/ (Exchange rate fetcher)       │
└─────────────────────────────────────────────────┘
```

## Tech Stack

- **Language**: Go 1.25+
- **Bot Framework**: [telebot.v4](https://gopkg.in/telebot.v4)
- **Precision Math**: [shopspring/decimal](https://github.com/shopspring/decimal)
- **CLI**: [urfave/cli/v3](https://github.com/urfave/cli)
- **Database**: Notion API
- **Exchange Rates**: FinMind API
- **Deployment**: Docker, Kubernetes (Helm)

## Getting Started

### Prerequisites

- Go 1.25 or later
- A Telegram Bot token (from [@BotFather](https://t.me/BotFather))
- A Notion integration token and database

### Notion Database Setup

Create a Notion database with the following properties:

| Property    | Type   | Description                        |
|-------------|--------|------------------------------------|
| name        | Title  | Expense name                       |
| price       | Number | Amount in source currency          |
| currency    | Select | TWD, JPY                           |
| category    | Select | 食, 衣, 住, 行, 樂                 |
| method      | Select | cash, credit_card, ic_card, paypay |
| shopped_at  | Date   | Transaction date                   |
| paid_by     | People | Notion user who paid               |
| ex_rate     | Number | Exchange rate (for JPY expenses)   |

### Environment Variables

Create a `.env` file or set the following environment variables:

```bash
# Required
TELEGRAM_BOT_TOKEN=your_telegram_bot_token
NOTION_TOKEN=your_notion_integration_token
NOTION_DATABASE_ID=your_notion_database_id
USER_MAPPING=telegram_id1:notion_id1:nickname1,telegram_id2:notion_id2:nickname2

# Optional
WEBAPP_URL=https://your-webapp-url.com  # For Mini App integration
LOG_LEVEL=INFO                           # DEBUG, INFO, WARN, ERROR
PORT=8080                                # HTTP server port (webapp only)
```

### Running Locally

**Telegram Bot:**
```bash
go run cmd/bot/main.go \
  --telegram-token=$TELEGRAM_BOT_TOKEN \
  --notion-token=$NOTION_TOKEN \
  --notion-db-id=$NOTION_DATABASE_ID \
  --user-mapping=$USER_MAPPING
```

**Web App:**
```bash
go run cmd/webapp/main.go \
  --telegram-token=$TELEGRAM_BOT_TOKEN \
  --notion-token=$NOTION_TOKEN \
  --notion-db-id=$NOTION_DATABASE_ID \
  --user-mapping=$USER_MAPPING \
  --port=8080
```

## Bot Commands

| Command    | Description                                    |
|------------|------------------------------------------------|
| `/start`   | Welcome message and Mini App button            |
| `/help`    | Show available commands                        |
| `/add`     | Quick add: `/add <name> <price> <currency> <category> <method>` |
| `/quick`   | Interactive step-by-step expense entry         |
| `/list`    | Show all expenses                              |
| `/summary` | Expense summary grouped by payer               |
| `/today`   | Today's expenses grouped by category           |
| `/edit`    | Modify recent expenses                         |
| `/cancel`  | Cancel current conversation                    |

## Web API Endpoints

| Method | Endpoint       | Description                        |
|--------|----------------|------------------------------------|
| GET    | `/`            | Serves the Mini App HTML           |
| GET    | `/api/auth`    | Validates Telegram WebApp initData |
| GET    | `/api/users`   | Lists authorized users             |
| POST   | `/api/expense` | Creates a new expense              |
| GET    | `/health`      | Health check endpoint              |

## Docker

### Building Images

**Bot:**
```bash
docker build -f build/bot/Dockerfile -t noccounting-bot .
```

**Web App:**
```bash
docker build -f build/webapp/Dockerfile -t noccounting-web .
```

### Running Containers

```bash
# Bot
docker run -d \
  -e TELEGRAM_BOT_TOKEN=$TELEGRAM_BOT_TOKEN \
  -e NOTION_TOKEN=$NOTION_TOKEN \
  -e NOTION_DATABASE_ID=$NOTION_DATABASE_ID \
  -e USER_MAPPING=$USER_MAPPING \
  noccounting-bot

# Web App
docker run -d -p 8080:8080 \
  -e TELEGRAM_BOT_TOKEN=$TELEGRAM_BOT_TOKEN \
  -e NOTION_TOKEN=$NOTION_TOKEN \
  -e NOTION_DATABASE_ID=$NOTION_DATABASE_ID \
  -e USER_MAPPING=$USER_MAPPING \
  noccounting-web
```

## Kubernetes Deployment

Deploy using Helm:

```bash
# Install
helm install noccounting ./helm \
  --set bot.telegramToken=$TELEGRAM_BOT_TOKEN \
  --set notion.token=$NOTION_TOKEN \
  --set notion.databaseId=$NOTION_DATABASE_ID \
  --set userMapping=$USER_MAPPING

# Upgrade
helm upgrade noccounting ./helm -f custom-values.yaml
```

### Helm Values

Key configuration options in `values.yaml`:

```yaml
bot:
  replicas: 1
  resources:
    requests:
      cpu: 50m
      memory: 50Mi

webapp:
  replicas: 1
  port: 8080
  resources:
    requests:
      cpu: 50m
      memory: 50Mi

ingress:
  enabled: true
  host: your-domain.com
  tls:
    enabled: true
```

## Development

### Project Structure

```
.
├── cmd/
│   ├── bot/main.go           # Bot entrypoint
│   └── webapp/main.go        # Web app entrypoint
├── domain/                   # Business domain models & interfaces
│   ├── accounting.go         # Expense model & repository interface
│   ├── category.go           # Expense categories enum
│   ├── currency.go           # Currency enum (TWD, JPY)
│   ├── payment_method.go     # Payment method enum
│   └── user.go               # User model & repository interface
├── internal/
│   ├── app/
│   │   ├── bot/              # Telegram bot handlers
│   │   └── webapp/           # HTTP handlers & templates
│   ├── infrastructure/       # External service clients
│   │   └── finmind/          # Exchange rate fetcher
│   ├── persistence/          # Data persistence
│   │   ├── notion/           # Notion API client
│   │   └── user/             # In-memory user repository
│   └── service/              # Business services
├── build/                    # Dockerfiles
├── helm/                     # Kubernetes Helm chart
└── .github/workflows/        # CI/CD pipelines
```

### Code Generation

Generate enum code:

```bash
go generate ./...
```

### Building

```bash
# Bot
go build -o bin/bot cmd/bot/main.go

# Web App
go build -o bin/webapp cmd/webapp/main.go
```

## License

This project is for personal use.

## Acknowledgments

- [Notion API](https://developers.notion.com/) for the backend database
- [Telegram Bot API](https://core.telegram.org/bots/api) for the bot interface
- [FinMind](https://finmindtrade.com/) for Taiwan exchange rate data
