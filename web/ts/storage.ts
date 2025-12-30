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
  const methodInput = get("method-input");
  const paidBySelect = get("paid-by-select");

  if (currencyInput) localStorage.setItem(STORAGE_KEYS.currency, currencyInput.value);
  if (categoryInput) localStorage.setItem(STORAGE_KEYS.category, categoryInput.value);
  if (methodInput) localStorage.setItem(STORAGE_KEYS.method, methodInput.value);
  if (paidBySelect) localStorage.setItem(STORAGE_KEYS.paidBy, paidBySelect.value);
}

function selectTab(tabsId: string, value: string): void {
  const trigger = document.querySelector(
    `#${tabsId} [data-tui-tabs-trigger][data-tui-tabs-value="${value}"]`,
  ) as HTMLElement | null;
  if (trigger) trigger.click();
}

export function restoreDefaults(
  updateExchangeRateVisibility: () => void
): void {
  const get = (id: string) => document.getElementById(id) as HTMLInputElement | HTMLSelectElement | null;

  const savedCurrency = localStorage.getItem(STORAGE_KEYS.currency);
  if (savedCurrency) {
    const input = get("currency-input");
    if (input) input.value = savedCurrency;
    selectTab("currency-tabs", savedCurrency);
  }

  const savedCategory = localStorage.getItem(STORAGE_KEYS.category);
  if (savedCategory) {
    const input = get("category-input");
    if (input) input.value = savedCategory;
    selectTab("category-tabs", savedCategory);
  }

  const savedMethod = localStorage.getItem(STORAGE_KEYS.method);
  if (savedMethod) {
    const input = get("method-input");
    if (input) input.value = savedMethod;
    selectTab("method-tabs", savedMethod);
  }

  const savedPaidBy = localStorage.getItem(STORAGE_KEYS.paidBy);
  const paidBySelect = get("paid-by-select") as HTMLSelectElement | null;
  if (paidBySelect && savedPaidBy) {
    paidBySelect.value = savedPaidBy;
  }

  const dateInput = get("date-input") as HTMLInputElement | null;
  if (dateInput) dateInput.value = new Date().toISOString().split("T")[0];

  updateExchangeRateVisibility();
}
