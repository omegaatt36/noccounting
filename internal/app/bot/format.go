package bot

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/format"
)

func expenseCard(heading string, trip domain.Trip, exp *domain.Expense) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n🧳 %s\n\n", heading, trip.Title)
	fmt.Fprintf(&sb, "📝 %s\n", exp.Name)
	fmt.Fprintf(&sb, "💰 %s%s\n", format.Money(exp.Currency, exp.PriceDecimal()), convertedSuffix(trip, exp))
	fmt.Fprintf(&sb, "📂 %s\n", format.Category(exp.Category))
	fmt.Fprintf(&sb, "💳 %s", format.PaymentLabel(exp.Method))
	return sb.String()
}

func convertedSuffix(trip domain.Trip, exp *domain.Expense) string {
	if exp.Currency == trip.Currency || exp.ExchangeRate.IsZero() {
		return ""
	}
	return fmt.Sprintf("（≈%s）", format.Money(trip.Currency, exp.TotalInBase(trip.Currency)))
}

func tripLine(trip domain.Trip) string {
	return fmt.Sprintf("🧳 %s（%s）", trip.Title, trip.Currency)
}

func truncate(label string, limit int) string {
	runes := []rune(label)
	if len(runes) <= limit {
		return label
	}
	return string(runes[:limit-1]) + "…"
}

func signed(currency domain.Currency, amount decimal.Decimal) string {
	if amount.IsPositive() {
		return "+" + format.Money(currency, amount)
	}
	return format.Money(currency, amount)
}

func settlementMessage(trip domain.Trip, settlement *domain.Settlement, names map[string]string) string {
	nameOf := func(id, fallback string) string {
		if name, ok := names[id]; ok {
			return name
		}
		return fallback
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "📊 結算總覽\n%s\n\n", tripLine(trip))

	if len(settlement.Balances) == 0 {
		sb.WriteString("🎉 目前沒有任何人欠款\n")
	} else {
		sb.WriteString("👥 餘額（正數＝應收）\n")
		for _, balance := range settlement.Balances {
			fmt.Fprintf(&sb, "• %s  %s\n", nameOf(balance.UserID, balance.Name), signed(settlement.Currency, balance.Amount))
		}
	}

	if len(settlement.Transfers) > 0 {
		sb.WriteString("\n💸 建議轉帳\n")
		for _, transfer := range settlement.Transfers {
			fmt.Fprintf(&sb, "• %s → %s  %s\n",
				nameOf(transfer.FromID, transfer.FromName),
				nameOf(transfer.ToID, transfer.ToName),
				format.Money(settlement.Currency, transfer.Amount))
		}
	}

	if settlement.Unconverted > 0 {
		fmt.Fprintf(&sb, "\n⚠️ 有 %d 筆因為缺少匯率沒有計入\n", settlement.Unconverted)
	}
	return sb.String()
}

func rateMessage(quotes []domain.RateQuote) string {
	if len(quotes) == 0 {
		return "❌ 暫時無法取得匯率，請稍後再試"
	}

	var sb strings.Builder
	sb.WriteString("💱 匯率（台灣銀行現金賣出）\n\n")
	for _, quote := range quotes {
		fmt.Fprintf(&sb, "1 %s = %s\n", quote.From, format.CurrencySymbol(quote.To)+quote.Rate.StringFixed(4))
	}
	return sb.String()
}
