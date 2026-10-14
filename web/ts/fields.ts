// What the expense form's fields share between filing an expense, editing one
// and restoring the last-used defaults.
const $ = (id: string) => document.getElementById(id);

export function formatDate(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

export function setMoreCategoriesExpanded(expanded: boolean): void {
  const box = $("more-categories");
  const btn = $("more-categories-btn");
  if (!box || !btn) return;
  box.classList.toggle("hidden", !expanded);
  btn.setAttribute("aria-expanded", String(expanded));
  btn.textContent = expanded ? "收起分類 ▴" : "更多分類 ▾";
}

// Clicks the tab for a value and says whether there was one: a value saved by
// an older version names no tab, and restoring it into the hidden input anyway
// would post something the server refuses.
export function selectTab(tabsId: string, value: string): boolean {
  const trigger = document.querySelector(
    `#${tabsId} [data-tui-tabs-trigger][data-tui-tabs-value="${value}"]`,
  ) as HTMLElement | null;
  if (!trigger) return false;
  if (trigger.closest("#more-categories")) {
    setMoreCategoriesExpanded(true);
  }
  trigger.click();
  return true;
}

const CHIP_ON = ["bg-primary", "text-primary-foreground"];
const CHIP_OFF = ["bg-muted", "text-muted-foreground"];

function markChip(chip: HTMLElement | null, pressed: boolean): void {
  if (!chip) return;
  chip.setAttribute("aria-pressed", String(pressed));
  chip.classList.remove(...(pressed ? CHIP_OFF : CHIP_ON));
  chip.classList.add(...(pressed ? CHIP_ON : CHIP_OFF));
}

// Sets the day and lights the today or yesterday chip when it is one of them.
export function setDate(value: string): void {
  const input = $("date-input") as HTMLInputElement | null;
  if (input) input.value = value;

  const yesterday = new Date();
  yesterday.setDate(yesterday.getDate() - 1);
  markChip($("date-today-btn"), value === formatDate(new Date()));
  markChip($("date-yesterday-btn"), value === formatDate(yesterday));
}

// An expense is being edited, not filed, while the form holds the id of one.
export function editingId(): string {
  return ($("expense-id") as HTMLInputElement | null)?.value ?? "";
}

export function isEditing(): boolean {
  return editingId() !== "";
}

// Empties the fields an expense is typed into, and the errors they showed.
export function clearFields(): void {
  const name = $("name") as HTMLInputElement | null;
  const price = $("price") as HTMLInputElement | null;
  const receiptUrl = $("receipt-url") as HTMLInputElement | null;
  const receiptItems = $("receipt-items") as HTMLInputElement | null;
  if (name) name.value = "";
  if (price) price.value = "";
  if (receiptUrl) receiptUrl.value = "";
  if (receiptItems) receiptItems.value = "";

  const display = $("numpad-display");
  if (display) display.textContent = "0";
  const convert = $("numpad-convert");
  if (convert) convert.textContent = "";

  for (const field of [name, price]) {
    field?.classList.remove("border-destructive");
    field?.setAttribute("aria-invalid", "false");
  }
  $("name-error")?.classList.add("hidden");
  $("price-error")?.classList.add("hidden");
}
