import { apiUrl, tripId } from "./api.js";
import { updateExchangeRateVisibility } from "./exchange-rate.js";
import { clearFields, editingId, selectTab, setDate, formatDate } from "./fields.js";
import { restoreDefaults } from "./storage.js";
import { confirmAction, haptic, setMainButtonText } from "./telegram.js";
import type { TelegramContext } from "./telegram.js";
import { showToast } from "./toast.js";
import { switchView } from "./navigation.js";

const $ = (id: string) => document.getElementById(id);

// An expense as the server puts it on a row of the list.
interface EditData {
  id: string;
  name: string;
  price: number;
  currency: string;
  exchange_rate: string;
  category: string;
  method: string;
  paid_by: string;
  participants: string[];
  shopped_at: string;
}

// A payer outside the user mapping has no entry in the picker. This one stands
// for them, and posts nothing, which keeps them as they are.
const ORIGINAL_PAYER = "original-payer";

function setEditMode(editing: boolean, ctx: TelegramContext): void {
  const title = editing ? "編輯消費" : "新增消費";
  const label = editing ? "儲存變更" : "新增消費";
  const set = (id: string, text: string) => {
    const el = $(id);
    if (el) el.textContent = text;
  };
  set("form-title", title);
  set("submit-label", label);
  setMainButtonText(ctx, label);
  $("edit-bar")?.classList.toggle("hidden", !editing);
  $("receipt-row")?.classList.toggle("hidden", editing);
}

function fill(data: EditData): void {
  const set = (id: string, value: string) => {
    const field = $(id) as HTMLInputElement | null;
    if (field) field.value = value;
  };

  // The id comes first: while it is set the currency tab leaves the rate alone.
  set("expense-id", data.id);
  clearFields();
  set("name", data.name);
  set("price", String(data.price));
  const display = $("numpad-display");
  if (display) display.textContent = String(data.price);

  selectTab("currency-tabs", data.currency);
  selectTab("category-tabs", data.category);
  selectTab("method-tabs", data.method);

  set("exchange-rate-input", data.exchange_rate);
  const status = $("rate-status");
  if (status) status.textContent = data.exchange_rate ? "(原值)" : "";
  updateExchangeRateVisibility();

  const payer = $("paid-by-select") as HTMLSelectElement | null;
  if (payer) {
    const known = Array.from(payer.options).some((opt) => opt.value === data.paid_by);
    if (data.paid_by !== "" && known) {
      payer.value = data.paid_by;
    } else {
      const original = new Option("原付款人", "");
      original.dataset.role = ORIGINAL_PAYER;
      payer.add(original, 0);
      payer.value = "";
    }
  }

  const chosen = new Set(data.participants);
  for (const box of document.querySelectorAll<HTMLInputElement>('#participants input[name="participants"]')) {
    box.checked = chosen.size === 0 || chosen.has(box.value);
  }
  setDate(data.shopped_at);
}

function enterEdit(ctx: TelegramContext, data: EditData): void {
  fill(data);
  setEditMode(true, ctx);
  switchView("form");
}

// Back to filing a new expense: the form is emptied and set to the last-used
// defaults, as when the app was opened.
async function leaveEdit(ctx: TelegramContext): Promise<void> {
  const id = $("expense-id") as HTMLInputElement | null;
  if (id) id.value = "";
  clearFields();
  setEditMode(false, ctx);

  const rate = $("exchange-rate-input") as HTMLInputElement | null;
  if (rate) rate.value = "";
  for (const box of document.querySelectorAll<HTMLInputElement>('#participants input[name="participants"]')) {
    box.checked = true;
  }
  document.querySelector(`#paid-by-select option[data-role="${ORIGINAL_PAYER}"]`)?.remove();
  setDate(formatDate(new Date()));
  await restoreDefaults(updateExchangeRateVisibility);
}

async function deleteExpense(ctx: TelegramContext): Promise<void> {
  const id = editingId();
  if (!id || !(await confirmAction(ctx, "確定要刪除這筆消費嗎？"))) return;

  const name = ($("name") as HTMLInputElement | null)?.value ?? "";
  try {
    const res = await fetch(apiUrl("/api/expense", ctx, { id, trip_id: tripId() }), {
      method: "DELETE",
    });
    if (!res.ok) throw new Error(`Delete failed: ${res.status}`);
  } catch (e) {
    console.error("Failed to delete the expense:", e);
    haptic(ctx, "notification", "error");
    showToast("刪除失敗", "刪除消費失敗，請稍後再試");
    return;
  }

  haptic(ctx, "notification", "success");
  showToast("已刪除", name, "success");
  document.dispatchEvent(new CustomEvent("expense-saved"));
  await leaveEdit(ctx);
  switchView("list");
}

export function setupEdit(ctx: TelegramContext): void {
  document.addEventListener("click", (evt) => {
    const target = evt.target instanceof Element ? evt.target : null;

    const row = target?.closest<HTMLElement>("[data-edit]");
    if (row?.dataset.edit) {
      enterEdit(ctx, JSON.parse(row.dataset.edit) as EditData);
    } else if (target?.closest('[data-action="cancel-edit"]')) {
      void leaveEdit(ctx).then(() => switchView("list"));
    } else if (target?.closest('[data-action="delete-expense"]')) {
      void deleteExpense(ctx);
    }
  });

  // The form saved the changes: it is a form for new expenses again, and the
  // list it came from is next.
  document.addEventListener("expense-edited", () => {
    void leaveEdit(ctx).then(() => switchView("list"));
  });
}
