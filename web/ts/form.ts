import type { TelegramContext } from "./telegram.js";
import { haptic } from "./telegram.js";
import { saveDefaults } from "./storage.js";
import {
  updateExchangeRateVisibility,
  fetchExchangeRate,
} from "./exchange-rate.js";

const $ = (id: string) => document.getElementById(id);

export function updateButtonGroup(
  groupId: string,
  hiddenInputId: string,
  value: string,
): void {
  const group = $(groupId);
  const hiddenInput = $(hiddenInputId) as HTMLInputElement | null;
  if (!group || !hiddenInput) return;

  group.querySelectorAll("button").forEach((btn) => {
    if ((btn as HTMLButtonElement).dataset.value === value) {
      btn.classList.add("selected");
    } else {
      btn.classList.remove("selected");
    }
  });
  hiddenInput.value = value;
}

export function setupEventListeners(ctx: TelegramContext): void {
  document.querySelectorAll("#category-grid .category-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      haptic(ctx, "impact", "light");
      updateButtonGroup(
        "category-grid",
        "category-input",
        (btn as HTMLButtonElement).dataset.value || "",
      );
    });
  });

  document.querySelectorAll("#currency-toggle .currency-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      haptic(ctx, "impact", "light");
      updateButtonGroup(
        "currency-toggle",
        "currency-input",
        (btn as HTMLButtonElement).dataset.value || "",
      );
      updateExchangeRateVisibility();
    });
  });

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
    form.addEventListener("htmx:beforeRequest", () => {
      (submitBtn as HTMLButtonElement).disabled = true;
      submitBtn.querySelector(".btn-text")?.classList.add("hidden");
      submitBtn.querySelector(".btn-loading")?.classList.remove("hidden");
    });

    form.addEventListener("htmx:afterRequest", ((e: Event) => {
      const detail = (e as CustomEvent).detail;
      (submitBtn as HTMLButtonElement).disabled = false;
      submitBtn.querySelector(".btn-text")?.classList.remove("hidden");
      submitBtn.querySelector(".btn-loading")?.classList.add("hidden");

      if (detail.successful) {
        haptic(ctx, "notification", "success");
        saveDefaults();
        const nameInput = $("name") as HTMLInputElement | null;
        const priceInput = $("price") as HTMLInputElement | null;
        if (nameInput) nameInput.value = "";
        if (priceInput) priceInput.value = "";
        if (nameInput) nameInput.focus();
      } else {
        haptic(ctx, "notification", "error");
      }
    }) as EventListener);
  }

  // Auto-dismiss result after 8s
  const resultContainer = $("result");
  if (resultContainer) {
    const observer = new MutationObserver(() => {
      if (resultContainer.children.length > 0) {
        setTimeout(() => {
          resultContainer.replaceChildren();
        }, 8000);
      }
    });
    observer.observe(resultContainer, { childList: true });
  }
}
