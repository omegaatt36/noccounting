export const STORAGE_KEYS = {
  currency: "noccounting_currency",
  category: "noccounting_category",
  method: "noccounting_method",
  paidBy: "noccounting_paid_by",
  exchangeRate: "noccounting_exchange_rate",
  exchangeRateDate: "noccounting_exchange_rate_date",
} as const;

export function saveDefaults(): void {
  const get = (id: string) => document.getElementById(id) as HTMLInputElement | HTMLSelectElement | null;
  const currencyInput = get("currency-input");
  const categoryInput = get("category-input");
  const methodSelect = get("method-select");
  const paidBySelect = get("paid-by-select");

  if (currencyInput) localStorage.setItem(STORAGE_KEYS.currency, currencyInput.value);
  if (categoryInput) localStorage.setItem(STORAGE_KEYS.category, categoryInput.value);
  if (methodSelect) localStorage.setItem(STORAGE_KEYS.method, methodSelect.value);
  if (paidBySelect) localStorage.setItem(STORAGE_KEYS.paidBy, paidBySelect.value);
}

export function restoreDefaults(
  updateButtonGroup: (groupId: string, hiddenInputId: string, value: string) => void,
  updateExchangeRateVisibility: () => void
): void {
  const get = (id: string) => document.getElementById(id) as HTMLInputElement | HTMLSelectElement | null;

  const savedCurrency = localStorage.getItem(STORAGE_KEYS.currency);
  if (savedCurrency) {
    const input = get("currency-input");
    if (input) input.value = savedCurrency;
    updateButtonGroup("currency-toggle", "currency-input", savedCurrency);
  }

  const savedCategory = localStorage.getItem(STORAGE_KEYS.category);
  if (savedCategory) {
    const input = get("category-input");
    if (input) input.value = savedCategory;
    updateButtonGroup("category-grid", "category-input", savedCategory);
  }

  const savedMethod = localStorage.getItem(STORAGE_KEYS.method);
  if (savedMethod) {
    const select = get("method-select");
    if (select) select.value = savedMethod;
  }

  const dateInput = get("date-input") as HTMLInputElement | null;
  if (dateInput) dateInput.value = new Date().toISOString().split("T")[0];

  updateExchangeRateVisibility();
}
