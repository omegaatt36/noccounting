import type { TelegramContext } from "./telegram.js";
import { haptic, setupMainButton, setMainButtonLoading } from "./telegram.js";
import { saveDefaults } from "./storage.js";
import {
  updateExchangeRateVisibility,
  fetchExchangeRate,
} from "./exchange-rate.js";
import { tripCurrency, tripId, appReady } from "./api.js";
import { showToast } from "./toast.js";
import { setupChoiceGroups } from "./a11y.js";
import {
  clearFields,
  formatDate,
  isEditing,
  renewRequestId,
  selectTab,
  setDate,
  setMoreCategoriesExpanded,
} from "./fields.js";

const $ = (id: string) => document.getElementById(id);

function activeCategoryLabel(): string {
  const categoryInput = $("category-input") as HTMLInputElement | null;
  const val = categoryInput?.value || "food";
  const trigger = document.querySelector?.(
    `#category-tabs [data-tui-tabs-trigger][data-tui-tabs-value="${val}"]`,
  ) as HTMLElement | null;
  if (trigger?.dataset?.categoryLabel) {
    return trigger.dataset.categoryLabel;
  }
  const text = trigger?.textContent?.trim() ?? "";
  const parts = text.split(/\s+/);
  return parts[parts.length - 1] || "餐飲";
}

function validateField(
  fieldId: string,
  errorId: string,
  check: (value: string) => boolean,
): boolean {
  const field = document.getElementById(fieldId) as HTMLInputElement | null;
  const errorEl = document.getElementById(errorId);
  if (!field) return true;

  const isValid = check(field.value);
  field.classList.toggle("border-destructive", !isValid);
  field.setAttribute("aria-invalid", String(!isValid));
  errorEl?.classList.toggle("hidden", isValid);
  return isValid;
}

function validateName(): boolean {
  return validateField("name", "name-error", (v) => v.trim().length > 0);
}

function validatePrice(): boolean {
  return validateField("price", "price-error", (v) => {
    const n = parseInt(v, 10);
    return !isNaN(n) && n > 0;
  });
}

// An expense is shared between the people ticked, so with nobody ticked there is
// nobody to owe anything.
function validateParticipants(): boolean {
  const boxes = document.querySelectorAll<HTMLInputElement>('#participants input[name="participants"]');
  const ok = boxes.length === 0 || Array.from(boxes).some((box) => box.checked);
  document.getElementById("participants-error")?.classList.toggle("hidden", ok);
  return ok;
}

function ensureDefaultName(): void {
  const nameField = document.getElementById("name") as HTMLInputElement | null;
  if (nameField && nameField.value.trim().length === 0 && !isEditing()) {
    nameField.value = activeCategoryLabel();
  }
}

function validateForm(): boolean {
  ensureDefaultName();
  const nameOk = validateName();
  const priceOk = validatePrice();
  const participantsOk = validateParticipants();
  return nameOk && priceOk && participantsOk;
}

function failureMessage(status: number): string {
  switch (status) {
    case 0:
      return "網路連線有問題，內容已保留，請稍後再試";
    case 403:
      return "驗證失敗，請關閉後重新開啟";
    case 429:
      return "操作太頻繁，請稍候一分鐘再試";
    default:
      return "伺服器暫時有問題，內容已保留，請稍後再試";
  }
}

// A phone photo is several megabytes, far more than the receipt needs, and its
// orientation may sit in metadata the server does not read. Drawing it onto a
// canvas applies that orientation and shrinks it, by its shorter side so a long
// receipt keeps its width. Whatever cannot be decoded goes up as it is.
const RECEIPT_SHORT_EDGE = 1600;
const RECEIPT_LONG_EDGE = 4096;
const RECEIPT_QUALITY = 0.85;

async function shrinkReceipt(file: File): Promise<Blob> {
  try {
    const bitmap = await createImageBitmap(file, { imageOrientation: "from-image" });
    const scale = Math.min(
      1,
      RECEIPT_SHORT_EDGE / Math.min(bitmap.width, bitmap.height),
      RECEIPT_LONG_EDGE / Math.max(bitmap.width, bitmap.height),
    );
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    canvas.getContext("2d")?.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    bitmap.close();

    const blob = await new Promise<Blob | null>((resolve) =>
      canvas.toBlob(resolve, "image/jpeg", RECEIPT_QUALITY),
    );
    return blob ?? file;
  } catch {
    return file;
  }
}

function setupReceiptUpload(ctx: TelegramContext): void {
  const receiptFileInput = document.getElementById(
    "receipt-file-input",
  ) as HTMLInputElement | null;
  const receiptIndicator = document.getElementById(
    "receipt-scanning-indicator",
  );

  if (!receiptFileInput) return;

  receiptFileInput.addEventListener("change", async () => {
    const file = receiptFileInput.files?.[0];
    if (!file) return;

    receiptIndicator?.classList.remove("hidden");
    haptic(ctx, "impact", "medium");

    try {
      const formData = new FormData();
      formData.append("receipt", await shrinkReceipt(file), "receipt.jpg");
      if (ctx.initData) {
        formData.append("init_data", ctx.initData);
      }
      const currentTrip = tripId();
      if (currentTrip) {
        formData.append("trip_id", currentTrip);
      }

      const res = await fetch("/api/receipt/analyze", {
        method: "POST",
        body: formData,
      });

      const data = await res.json();
      if (!res.ok || !data.success) {
        showToast("辨識失敗", data.error || "收據辨識失敗，請手動輸入");
        haptic(ctx, "notification", "error");
        return;
      }

      if (data.summary) {
        const nameEl = document.getElementById("name") as HTMLInputElement | null;
        if (nameEl) nameEl.value = data.summary;
      }

      if (data.total) {
        const priceEl = document.getElementById("price") as HTMLInputElement | null;
        if (priceEl) {
          priceEl.value = String(data.total);
          const numpadDisplay = document.getElementById("numpad-display");
          if (numpadDisplay) numpadDisplay.textContent = String(data.total);
        }
      }

      const receiptTokenEl = document.getElementById("receipt-token") as HTMLInputElement | null;
      if (receiptTokenEl) receiptTokenEl.value = data.receipt_token || "";
      const receiptItemsEl = document.getElementById("receipt-items") as HTMLInputElement | null;
      if (receiptItemsEl) {
        receiptItemsEl.value =
          Array.isArray(data.receipt_items) && data.receipt_items.length > 0
            ? JSON.stringify(data.receipt_items)
            : "";
      }

      if (data.currency) {
        selectTab("currency-tabs", data.currency);
      }

      if (data.category) {
        selectTab("category-tabs", data.category);
      }

      if (data.method) {
        selectTab("method-tabs", data.method);
      }

      validateName();
      validatePrice();
      haptic(ctx, "notification", "success");
    } catch (err) {
      console.error("Receipt upload error:", err);
      showToast("上傳失敗", "上傳收據失敗，請稍後再試");
      haptic(ctx, "notification", "error");
    } finally {
      receiptIndicator?.classList.add("hidden");
      receiptFileInput.value = "";
    }
  });
}

export function setupEventListeners(ctx: TelegramContext): void {
  setupReceiptUpload(ctx);
  setupChoiceGroups();

  const todayBtn = document.getElementById("date-today-btn");
  const yesterdayBtn = document.getElementById("date-yesterday-btn");
  const dateInput = document.getElementById(
    "date-input",
  ) as HTMLInputElement | null;

  todayBtn?.addEventListener("click", () => {
    setDate(formatDate(new Date()));
    haptic(ctx, "impact", "light");
  });
  yesterdayBtn?.addEventListener("click", () => {
    const yesterday = new Date();
    yesterday.setDate(yesterday.getDate() - 1);
    setDate(formatDate(yesterday));
    haptic(ctx, "impact", "light");
  });

  if (dateInput && !dateInput.value) {
    setDate(formatDate(new Date()));
  }
  renewRequestId();

  // A Mini App left open overnight still shows yesterday as today. A date nobody
  // touched moves on when the app comes back; one that was picked stays.
  let autoDate = formatDate(new Date());
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState !== "visible" || isEditing()) return;
    const today = formatDate(new Date());
    if (today !== autoDate && dateInput?.value === autoDate) setDate(today);
    autoDate = today;
  });

  const moreCategoriesBtn = document.getElementById("more-categories-btn");
  moreCategoriesBtn?.addEventListener("click", () => {
    const expanded = moreCategoriesBtn.getAttribute("aria-expanded") === "true";
    setMoreCategoriesExpanded(!expanded);
    haptic(ctx, "impact", "light");
  });

  document
    .querySelectorAll("#category-tabs [data-tui-tabs-trigger]")
    .forEach((trigger) => {
      trigger.addEventListener("click", () => {
        const value = (trigger as HTMLElement).dataset.tuiTabsValue || "";
        const input = $("category-input") as HTMLInputElement | null;
        if (input) input.value = value;
        haptic(ctx, "impact", "light");
      });
    });

  document
    .querySelectorAll("#method-tabs [data-tui-tabs-trigger]")
    .forEach((trigger) => {
      trigger.addEventListener("click", () => {
        const value = (trigger as HTMLElement).dataset.tuiTabsValue || "";
        const input = $("method-input") as HTMLInputElement | null;
        if (input) input.value = value;
        haptic(ctx, "impact", "light");
      });
    });

  document
    .querySelectorAll("#currency-tabs [data-tui-tabs-trigger]")
    .forEach((trigger) => {
      trigger.addEventListener("click", () => {
        const value = (trigger as HTMLElement).dataset.tuiTabsValue || "";
        const input = $("currency-input") as HTMLInputElement | null;
        if (input) input.value = value;
        haptic(ctx, "impact", "light");
        updateExchangeRateVisibility();
        // The rate shown belongs to one direction of the pair; the new currency
        // needs its own. An expense being edited keeps the rate it was filed with.
        if (value !== tripCurrency() && !isEditing()) fetchExchangeRate(ctx);
      });
    });

  const nameInput = document.getElementById("name");
  if (nameInput) {
    nameInput.addEventListener("blur", () => {
      if (isEditing() || (nameInput as HTMLInputElement).value.trim().length > 0) {
        validateName();
      }
    });
  }

  const fetchRateBtn = $("fetch-rate-btn");
  if (fetchRateBtn) {
    fetchRateBtn.addEventListener("click", () => {
      haptic(ctx, "impact", "medium");
      fetchExchangeRate(ctx);
    });
  }

  const form = $("expense-form");
  const submitBtn = $("submit-btn");

  setupMainButton(ctx, () => {
    if (!appReady() || !validateForm()) return;
    setMainButtonLoading(ctx, true);
    (
      document.getElementById("expense-form") as HTMLFormElement
    )?.requestSubmit();
  });

  if (form && submitBtn) {
    form.addEventListener("submit", () => {
      ensureDefaultName();
    }, true);

    form.addEventListener("htmx:before:request", (e: Event) => {
      if (!appReady() || !validateForm()) {
        e.preventDefault();
        (submitBtn as HTMLButtonElement).disabled = !appReady();
        submitBtn.querySelector(".btn-text")?.classList.remove("hidden");
        submitBtn.querySelector(".btn-loading")?.classList.add("hidden");
        return;
      }
      (submitBtn as HTMLButtonElement).disabled = true;
      submitBtn.querySelector(".btn-text")?.classList.add("hidden");
      submitBtn.querySelector(".btn-loading")?.classList.remove("hidden");
      setMainButtonLoading(ctx, true);
    });

    form.addEventListener("htmx:finally:request", ((e: Event) => {
      const detail = (e as CustomEvent<{ ctx: { response?: { status: number } } }>).detail;
      (submitBtn as HTMLButtonElement).disabled = !appReady();
      submitBtn.querySelector(".btn-text")?.classList.remove("hidden");
      submitBtn.querySelector(".btn-loading")?.classList.add("hidden");
      setMainButtonLoading(ctx, false);

      const toastTrigger = $("toast-trigger");
      const success = toastTrigger?.dataset.success === "true";

      const status = detail.ctx.response?.status ?? 0;
      if (status >= 200 && status < 300 && success) {
        haptic(ctx, "notification", "success");
        document.dispatchEvent(new CustomEvent("expense-saved"));
        if (isEditing()) {
          document.dispatchEvent(new CustomEvent("expense-edited"));
        } else {
          saveDefaults();
          clearFields();
          $("name")?.focus();
        }
      } else {
        haptic(ctx, "notification", "error");
        // A 2xx answer carries its own error toast. Anything else was never
        // swapped in, so without this the form would just sit there.
        if (status < 200 || status >= 300) {
          showToast(isEditing() ? "儲存失敗" : "新增失敗", failureMessage(status));
        }
      }

      document.querySelectorAll("#result [data-tui-toast]").forEach((el) => {
        const duration = parseInt(
          el.getAttribute("data-tui-toast-duration") || "3000",
          10,
        );
        setTimeout(() => {
          (el as HTMLElement).style.transition =
            "opacity 300ms, transform 300ms";
          (el as HTMLElement).style.opacity = "0";
          (el as HTMLElement).style.transform = "translateY(-1rem)";
          setTimeout(() => el.remove(), 300);
        }, duration);
      });
    }) as EventListener);
  }
}
