package format

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

func CategoryEmoji(c domain.Category) string {
	switch c {
	case domain.CategoryFood:
		return "🍜"
	case domain.CategoryTransport:
		return "🚃"
	case domain.CategoryShopping:
		return "🛍️"
	case domain.CategoryActivities:
		return "🎯"
	case domain.CategoryAccommodation:
		return "🏠"
	case domain.CategorySightseeing:
		return "🗼"
	case domain.CategoryGroceries:
		return "🛒"
	case domain.CategoryFlights:
		return "✈️"
	case domain.CategoryFuel:
		return "⛽"
	case domain.CategoryParking:
		return "🅿️"
	case domain.CategoryFees:
		return "🎟️"
	case domain.CategoryHealth:
		return "💊"
	case domain.CategoryTips:
		return "💴"
	case domain.CategoryOther:
		return "📎"
	default:
		return "❓"
	}
}

func CategoryLabel(c domain.Category) string {
	switch c {
	case domain.CategoryFood:
		return "餐飲"
	case domain.CategoryTransport:
		return "交通"
	case domain.CategoryShopping:
		return "購物"
	case domain.CategoryActivities:
		return "娛樂"
	case domain.CategoryAccommodation:
		return "住宿"
	case domain.CategorySightseeing:
		return "景點"
	case domain.CategoryGroceries:
		return "超市"
	case domain.CategoryFlights:
		return "機票"
	case domain.CategoryFuel:
		return "油資"
	case domain.CategoryParking:
		return "停車"
	case domain.CategoryFees:
		return "票券手續費"
	case domain.CategoryHealth:
		return "醫療"
	case domain.CategoryTips:
		return "小費"
	case domain.CategoryOther:
		return "其他"
	default:
		return string(c)
	}
}

func Category(c domain.Category) string {
	return CategoryEmoji(c) + " " + CategoryLabel(c)
}

func PaymentEmoji(p domain.PaymentMethod) string {
	switch p {
	case domain.PaymentMethodCash:
		return "💵"
	case domain.PaymentMethodCreditCard:
		return "💳"
	case domain.PaymentMethodIcCard:
		return "🎫"
	case domain.PaymentMethodEPay:
		return "📱"
	default:
		return "❔"
	}
}

func PaymentLabel(p domain.PaymentMethod) string {
	switch p {
	case domain.PaymentMethodCash:
		return "現金"
	case domain.PaymentMethodCreditCard:
		return "信用卡"
	case domain.PaymentMethodIcCard:
		return "IC卡"
	case domain.PaymentMethodEPay:
		return "電子支付"
	default:
		return string(p)
	}
}

func CurrencySymbol(c domain.Currency) string {
	switch c {
	case domain.CurrencyTWD:
		return "NT$"
	case domain.CurrencyJPY:
		return "¥"
	default:
		return string(c) + " "
	}
}

func Money(c domain.Currency, amount decimal.Decimal) string {
	rounded := amount.Round(0)
	sign := ""
	if rounded.IsNegative() {
		sign = "-"
		rounded = rounded.Abs()
	}

	digits := rounded.String()
	var grouped strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	return sign + CurrencySymbol(c) + grouped.String()
}

func ParseCategoryInput(input string) (domain.Category, error) {
	input = strings.TrimSpace(input)
	if category, err := domain.ParseCategory(strings.ToLower(input)); err == nil {
		return category, nil
	}
	for _, category := range domain.CategoryValues() {
		if CategoryLabel(category) == input {
			return category, nil
		}
	}
	return "", fmt.Errorf("%s is %w", input, domain.ErrInvalidCategory)
}

func ParsePaymentMethodInput(input string) (domain.PaymentMethod, error) {
	input = strings.TrimSpace(input)
	if method, err := domain.ParsePaymentMethod(strings.ToLower(input)); err == nil {
		return method, nil
	}
	for _, method := range domain.PaymentMethodValues() {
		if PaymentLabel(method) == input {
			return method, nil
		}
	}
	return "", fmt.Errorf("%s is %w", input, domain.ErrInvalidPaymentMethod)
}

func Trip(t domain.Trip) string {
	if !t.HasDates() {
		return t.Title
	}
	return fmt.Sprintf("%s %s–%s", t.Title, t.StartDate.Format("01/02"), t.EndDate.Format("01/02"))
}

func ReceiptName(r domain.ReceiptItem) string {
	if r.NameZH != "" {
		return r.Name + "（" + r.NameZH + "）"
	}
	return r.Name
}
