package domain

// ReceiptItem represents a single item extracted from a receipt.
type ReceiptItem struct {
	Name     string
	NameZH   string `json:"name_zh"` // Traditional Chinese translation (empty if already Chinese)
	Price    uint64
	Category Category
}

// DisplayName returns the item name with Chinese translation if available.
func (r ReceiptItem) DisplayName() string {
	if r.NameZH != "" {
		return r.Name + "（" + r.NameZH + "）"
	}
	return r.Name
}

// ReceiptAnalysis represents the result of analyzing a receipt image.
type ReceiptAnalysis struct {
	Summary  string
	Items    []ReceiptItem
	Currency Currency
	Total    uint64
}
