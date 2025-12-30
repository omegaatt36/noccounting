package domain

import "context"

// ReceiptItem represents a single item extracted from a receipt.
type ReceiptItem struct {
	Name     string
	NameZH   string `json:"name_zh"` // Traditional Chinese translation (empty if already Chinese)
	Price    uint64
	Category Category
}

// DisplayName returns the item name with Chinese translation if available.
// Format: "原文（中文翻譯）" for non-Chinese items, or just the name if already Chinese.
func (r ReceiptItem) DisplayName() string {
	if r.NameZH != "" {
		return r.Name + "（" + r.NameZH + "）"
	}
	return r.Name
}

// ReceiptAnalysis represents the result of analyzing a receipt image.
type ReceiptAnalysis struct {
	Summary  string // Short readable name, e.g. "松屋 午餐", "全家便利商店"
	Items    []ReceiptItem
	Currency Currency
	Total    uint64
}

// ReceiptAnalyzer analyzes receipt images and extracts expense data.
type ReceiptAnalyzer interface {
	Analyze(ctx context.Context, imageData []byte) (*ReceiptAnalysis, error)
}
