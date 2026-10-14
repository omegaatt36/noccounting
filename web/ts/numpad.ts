import type { TelegramContext } from "./telegram.js";
import { haptic } from "./telegram.js";
import { appReady, symbol, tripCurrency } from "./api.js";

export function setupNumpad(ctx: TelegramContext): void {
  const priceField = document.getElementById("price") as HTMLInputElement | null;
  const numpad = document.getElementById("numpad-sheet");
  const backdrop = document.getElementById("numpad-backdrop");
  const numpadDisplay = document.getElementById("numpad-display");
  const numpadConvert = document.getElementById("numpad-convert");
  const numpadConfirm = document.getElementById("numpad-confirm");
  const numpadClear = document.getElementById("numpad-clear");

  if (!priceField || !numpad) return;

  // Prevent system keyboard
  priceField.setAttribute("inputmode", "none");
  priceField.setAttribute("readonly", "true");

  // Telegram's back button closes the sheet instead of closing the whole app,
  // and the MainButton hides while typing so a stray tap cannot submit a
  // half-typed amount.
  const backButton = ctx.tg?.BackButton;
  backButton?.onClick(closeNumpad);
  backdrop?.addEventListener("click", closeNumpad);

  numpadClear?.addEventListener("click", () => {
    priceField.value = "";
    updateDisplay();
    haptic(ctx, "impact", "light");
  });

  priceField.addEventListener("click", () => openNumpad());
  // The field is read-only and opens a sheet, so Enter and Space must do what a
  // click does for keyboard and switch users.
  priceField.addEventListener("keydown", (e) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      openNumpad();
    }
  });
  // Escape closes the sheet wherever focus is; with the sheet closed it is a no-op.
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") closeNumpad();
  });

  function isOpen(): boolean {
    return !numpad!.classList.contains("hidden");
  }

  function openNumpad(): void {
    if (isOpen()) return;
    numpad!.classList.remove("hidden");
    numpad!.classList.add("flex");
    backdrop?.classList.remove("hidden");
    priceField!.setAttribute("aria-expanded", "true");
    ctx.tg?.MainButton.hide();
    updateDisplay();
    backButton?.show();
    // Focus moves into the sheet so a screen reader reads it and Tab stays there.
    // Not before the key that opened it has finished: its keypress would land on
    // the confirm button and close the sheet again.
    setTimeout(() => numpadConfirm?.focus(), 0);
  }

  function closeNumpad(): void {
    if (!isOpen()) return;
    numpad!.classList.add("hidden");
    numpad!.classList.remove("flex");
    backdrop?.classList.add("hidden");
    priceField!.setAttribute("aria-expanded", "false");
    backButton?.hide();
    if (appReady()) ctx.tg?.MainButton.show();
    priceField!.focus();
  }

  function updateDisplay(): void {
    const val = priceField!.value || "0";
    if (numpadDisplay) numpadDisplay.textContent = Number(val).toLocaleString();

    const currencyInput = document.getElementById("currency-input") as HTMLInputElement | null;
    const currency = currencyInput?.value ?? "TWD";

    // Update currency label
    const currencyLabel = document.getElementById("numpad-currency-label");
    if (currencyLabel) currencyLabel.textContent = currency;

    // Live conversion into the trip's currency, for an expense in another one
    const rateInput = document.getElementById("exchange-rate-input") as HTMLInputElement | null;
    const base = tripCurrency();
    const rate = parseFloat(rateInput?.value ?? "");
    if (numpadConvert && base !== "" && currency !== base && !isNaN(rate)) {
      const converted = Math.round(parseFloat(val) * rate);
      numpadConvert.textContent = isNaN(converted) ? "" : `≈ ${symbol(base)}${converted.toLocaleString()}`;
    } else if (numpadConvert) {
      numpadConvert.textContent = "";
    }
  }

  // Observe currency / exchange rate changes for live conversion update
  const rateInput = document.getElementById("exchange-rate-input") as HTMLInputElement | null;
  rateInput?.addEventListener("input", updateDisplay);

  const currencyInput = document.getElementById("currency-input") as HTMLInputElement | null;
  currencyInput?.addEventListener("change", updateDisplay);

  // also update on tabs change
  document.querySelectorAll("#currency-tabs [data-tui-tabs-trigger]").forEach((trigger) => {
    trigger.addEventListener("click", () => {
       setTimeout(updateDisplay, 10);
    });
  });

  // Key press handler
  document.querySelectorAll("[data-numpad-key]").forEach(btn => {
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      haptic(ctx, "impact", "light");
      const key = (btn as HTMLElement).dataset.numpadKey!;
      const current = priceField!.value;

      if (key === "backspace") {
        priceField!.value = current.slice(0, -1);
      } else if (key === ".") {
        if (!current.includes(".")) priceField!.value = current + ".";
      } else {
        priceField!.value = (current === "0" ? "" : current) + key;
      }
      updateDisplay();
    });
  });

  numpadConfirm?.addEventListener("click", closeNumpad);
}
