import type { TelegramContext } from "./telegram.js";
import { haptic } from "./telegram.js";
import { saveDefaults } from "./storage.js";
import {
  updateExchangeRateVisibility,
  fetchExchangeRate,
} from "./exchange-rate.js";

const $ = (id: string) => document.getElementById(id);

function showToast(
  success: boolean,
  name: string,
  price: string,
  currency: string,
  categoryEmoji: string,
  twdAmount: string,
  errorMsg: string,
): void {
  const position = "top-center";
  const variant = success ? "success" : "error";
  const duration = 3000;

  let title = "";
  let description = "";

  if (success) {
    title = `${categoryEmoji} ${name}`;
    if (currency === "JPY" && twdAmount) {
      description = `¥${price} (≈ NT$${twdAmount})`;
    } else {
      description = `${currency} ${price}`;
    }
  } else {
    title = "⚠️ 新增失敗";
    description = errorMsg || "發生未知錯誤";
  }

  const toastEl = document.createElement("div");
  toastEl.setAttribute("data-tui-toast", "");
  toastEl.setAttribute("data-tui-toast-duration", String(duration));
  toastEl.setAttribute("data-position", position);
  toastEl.setAttribute("data-variant", variant);
  toastEl.className =
    "z-50 fixed pointer-events-auto p-4 w-full md:max-w-[420px] " +
    "animate-in fade-in slide-in-from-bottom-4 duration-300 " +
    "data-[position=top-center]:top-0 data-[position=top-center]:left-1/2 data-[position=top-center]:-translate-x-1/2 " +
    "data-[position*=top]:slide-in-from-top-4";

  const innerEl = document.createElement("div");
  innerEl.className =
    "w-full bg-popover text-popover-foreground rounded-lg shadow-xs border pt-5 pb-4 px-4 flex items-center justify-center relative overflow-hidden group";

  const contentEl = document.createElement("span");
  contentEl.className = "flex-1 min-w-0";

  const titleEl = document.createElement("p");
  titleEl.className = "text-sm font-semibold truncate";
  titleEl.textContent = title;

  const descEl = document.createElement("p");
  descEl.className = "text-sm opacity-90 mt-1";
  descEl.textContent = description;

  contentEl.appendChild(titleEl);
  contentEl.appendChild(descEl);
  innerEl.appendChild(contentEl);
  toastEl.appendChild(innerEl);

  document.body.appendChild(toastEl);

  // Auto-remove after duration
  setTimeout(() => {
    toastEl.style.transition = "opacity 300ms, transform 300ms";
    toastEl.style.opacity = "0";
    toastEl.style.transform = "translateY(1rem)";
    setTimeout(() => toastEl.remove(), 300);
  }, duration);
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

        // Show toast from trigger data
        const toastTrigger = $("toast-trigger");
        if (toastTrigger) {
          const success = toastTrigger.dataset.success === "true";
          const name = toastTrigger.dataset.name || "";
          const price = toastTrigger.dataset.price || "";
          const currency = toastTrigger.dataset.currency || "";
          const categoryEmoji = toastTrigger.dataset.categoryEmoji || "";
          const twdAmount = toastTrigger.dataset.twdAmount || "";
          const errorMsg = toastTrigger.dataset.error || "";

          showToast(
            success,
            name,
            price,
            currency,
            categoryEmoji,
            twdAmount,
            errorMsg,
          );
        }
      } else {
        haptic(ctx, "notification", "error");

        // Show error toast
        const toastTrigger = $("toast-trigger");
        if (toastTrigger) {
          const errorMsg = toastTrigger.dataset.error || "發生未知錯誤";
          showToast(false, "", "", "", "", "", errorMsg);
        }
      }
    }) as EventListener);
  }
}
