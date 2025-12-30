import type { TelegramContext } from "./telegram.js";
import { haptic } from "./telegram.js";
import { saveDefaults } from "./storage.js";
import {
  updateExchangeRateVisibility,
  fetchExchangeRate,
} from "./exchange-rate.js";

const $ = (id: string) => document.getElementById(id);

function validateField(fieldId: string, errorId: string, check: (value: string) => boolean): boolean {
  const field = document.getElementById(fieldId) as HTMLInputElement | null;
  const errorEl = document.getElementById(errorId);
  if (!field) return true;

  const isValid = check(field.value);
  if (isValid) {
    field.classList.remove("border-destructive");
    if (errorEl) errorEl.classList.add("hidden");
  } else {
    field.classList.add("border-destructive");
    if (errorEl) errorEl.classList.remove("hidden");
  }
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

function validateForm(): boolean {
  const nameOk = validateName();
  const priceOk = validatePrice();
  return nameOk && priceOk;
}

export function setupEventListeners(ctx: TelegramContext): void {
  // Category tabs listener
  document.querySelectorAll("#category-tabs [data-tui-tabs-trigger]").forEach((trigger) => {
    trigger.addEventListener("click", () => {
      const value = (trigger as HTMLElement).dataset.tuiTabsValue || "";
      const input = $("category-input") as HTMLInputElement | null;
      if (input) input.value = value;
      haptic(ctx, "impact", "light");
    });
  });

  // Payment method tabs listener
  document.querySelectorAll("#method-tabs [data-tui-tabs-trigger]").forEach((trigger) => {
    trigger.addEventListener("click", () => {
      const value = (trigger as HTMLElement).dataset.tuiTabsValue || "";
      const input = $("method-input") as HTMLInputElement | null;
      if (input) input.value = value;
      haptic(ctx, "impact", "light");
    });
  });

  // Currency tabs listener
  document.querySelectorAll("#currency-tabs [data-tui-tabs-trigger]").forEach((trigger) => {
    trigger.addEventListener("click", () => {
      const value = (trigger as HTMLElement).dataset.tuiTabsValue || "";
      const input = $("currency-input") as HTMLInputElement | null;
      if (input) input.value = value;
      haptic(ctx, "impact", "light");
      updateExchangeRateVisibility();
    });
  });

  // Name field blur validation
  const nameInput = document.getElementById("name");
  if (nameInput) {
    nameInput.addEventListener("blur", validateName);
  }

  // Price field blur validation
  const priceInputEl = document.getElementById("price");
  if (priceInputEl) {
    priceInputEl.addEventListener("blur", validatePrice);
  }

  const fetchRateBtn = $("fetch-rate-btn");
  if (fetchRateBtn) {
    fetchRateBtn.addEventListener("click", () => {
      haptic(ctx, "impact", "medium");
      fetchExchangeRate();
    });
  }

  const form = $("expense-form");
  const submitBtn = $("submit-btn");

  if (form && submitBtn) {
    form.addEventListener("htmx:beforeRequest", (e: Event) => {
      if (!validateForm()) {
        (e as CustomEvent).detail.shouldSwap = false;
        e.preventDefault();
        (submitBtn as HTMLButtonElement).disabled = false;
        submitBtn.querySelector(".btn-text")?.classList.remove("hidden");
        submitBtn.querySelector(".btn-loading")?.classList.add("hidden");
        return;
      }
      (submitBtn as HTMLButtonElement).disabled = true;
      submitBtn.querySelector(".btn-text")?.classList.add("hidden");
      submitBtn.querySelector(".btn-loading")?.classList.remove("hidden");
    });

    form.addEventListener("htmx:afterRequest", ((e: Event) => {
      const detail = (e as CustomEvent).detail;
      (submitBtn as HTMLButtonElement).disabled = false;
      submitBtn.querySelector(".btn-text")?.classList.remove("hidden");
      submitBtn.querySelector(".btn-loading")?.classList.add("hidden");

      // Read success state for haptic
      const toastTrigger = $("toast-trigger");
      const success = toastTrigger?.dataset.success === "true";

      if (detail.successful && success) {
        haptic(ctx, "notification", "success");
        saveDefaults();
        const nameInput = $("name") as HTMLInputElement | null;
        const priceInput = $("price") as HTMLInputElement | null;
        if (nameInput) nameInput.value = "";
        if (priceInput) priceInput.value = "";
        if (nameInput) nameInput.focus();
        // Clear validation errors
        nameInput?.classList.remove("border-destructive");
        priceInput?.classList.remove("border-destructive");
        $("name-error")?.classList.add("hidden");
        $("price-error")?.classList.add("hidden");
      } else {
        haptic(ctx, "notification", "error");
      }

      // Auto-dismiss templui toast after duration
      document.querySelectorAll("#result [data-tui-toast]").forEach((el) => {
        const duration = parseInt(el.getAttribute("data-tui-toast-duration") || "3000", 10);
        setTimeout(() => {
          (el as HTMLElement).style.transition = "opacity 300ms, transform 300ms";
          (el as HTMLElement).style.opacity = "0";
          (el as HTMLElement).style.transform = "translateY(-1rem)";
          setTimeout(() => el.remove(), 300);
        }, duration);
      });
    }) as EventListener);
  }
}
