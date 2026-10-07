package domain

type ReceiptItem struct {
	Name     string
	NameZH   string // Traditional Chinese translation (empty if already Chinese)
	Price    int64
	Category Category
}

type ReceiptAnalysis struct {
	Summary       string
	Items         []ReceiptItem
	Currency      Currency
	Total         uint64
	Category      Category
	PaymentMethod PaymentMethod
}
