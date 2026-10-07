export const STORAGE_KEYS = {
  currency: "noccounting_currency",
  category: "noccounting_category",
  method: "noccounting_method",
  paidBy: "noccounting_paid_by",
  exchangeRate: "noccounting_exchange_rate",
} as const;

// Telegram CloudStorage keeps defaults in sync across the user's devices and
// survives iOS webview eviction; localStorage covers dev mode and clients
// without it.
function cloudStorage(): TelegramCloudStorage | null {
  return window.Telegram?.WebApp?.CloudStorage ?? null;
}

export function storageGet(key: string): Promise<string | null> {
  const cloud = cloudStorage();
  if (!cloud) return Promise.resolve(localStorage.getItem(key));
  return new Promise((resolve) => {
    cloud.getItem(key, (error, value) => {
      resolve(error ? null : (value ?? null));
    });
  });
}

export function storageSet(key: string, value: string): void {
  const cloud = cloudStorage();
  if (cloud) {
    cloud.setItem(key, value);
    return;
  }
  localStorage.setItem(key, value);
}

// Preferences that depend on where the trip is (the currency) or who is in it
// (the payer) must not leak between trips.
function tripScopedKey(key: string): string {
  const tripId = document.body.dataset.tripId ?? "";
  return tripId ? `${key}_${tripId}` : key;
}

export function saveDefaults(): void {
  const get = (id: string) => document.getElementById(id) as HTMLInputElement | HTMLSelectElement | null;
  const currencyInput = get("currency-input");
  const categoryInput = get("category-input");
  const methodInput = get("method-input");
  const paidBySelect = get("paid-by-select");

  if (currencyInput) storageSet(tripScopedKey(STORAGE_KEYS.currency), currencyInput.value);
  if (categoryInput) storageSet(STORAGE_KEYS.category, categoryInput.value);
  if (methodInput) storageSet(STORAGE_KEYS.method, methodInput.value);
  if (paidBySelect) storageSet(tripScopedKey(STORAGE_KEYS.paidBy), paidBySelect.value);
}

// Clicks the tab for a saved value and says whether there was one: a value saved
// by an older version (the categories used to be 食, 住, ...) names no tab, and
// restoring it into the hidden input anyway would post a category the server
// refuses.
function selectTab(tabsId: string, value: string): boolean {
  const trigger = document.querySelector(
    `#${tabsId} [data-tui-tabs-trigger][data-tui-tabs-value="${value}"]`,
  ) as HTMLElement | null;
  if (!trigger) return false;
  trigger.click();
  return true;
}

export async function restoreDefaults(
  updateExchangeRateVisibility: () => void
): Promise<void> {
  const get = (id: string) => document.getElementById(id) as HTMLInputElement | HTMLSelectElement | null;

  // Last-used currency for this trip, falling back to the trip's own currency:
  // a trip in a new country should not inherit the previous one's money.
  const tripCurrency = document.body.dataset.tripCurrency ?? "";
  let currency = "JPY";
  for (const candidate of [
    await storageGet(tripScopedKey(STORAGE_KEYS.currency)),
    tripCurrency,
  ]) {
    if (candidate && selectTab("currency-tabs", candidate)) {
      currency = candidate;
      break;
    }
  }
  const currencyInput = get("currency-input");
  if (currencyInput) currencyInput.value = currency;

  const savedCategory = await storageGet(STORAGE_KEYS.category);
  if (savedCategory && selectTab("category-tabs", savedCategory)) {
    const input = get("category-input");
    if (input) input.value = savedCategory;
  }

  const savedMethod = await storageGet(STORAGE_KEYS.method);
  if (savedMethod && selectTab("method-tabs", savedMethod)) {
    const input = get("method-input");
    if (input) input.value = savedMethod;
  }

  // Reuse a payer only while they are still in this trip's member list;
  // otherwise the select keeps the current user chosen by loadUsers.
  const savedPaidBy = await storageGet(tripScopedKey(STORAGE_KEYS.paidBy));
  const paidBySelect = get("paid-by-select") as HTMLSelectElement | null;
  if (paidBySelect && savedPaidBy) {
    const known = Array.from(paidBySelect.options).some(
      (opt) => opt.value === savedPaidBy,
    );
    if (known) paidBySelect.value = savedPaidBy;
  }

  updateExchangeRateVisibility();
}
