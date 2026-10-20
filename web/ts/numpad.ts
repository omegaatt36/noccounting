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

  priceField.setAttribute("inputmode", "none");
  priceField.setAttribute("readonly", "true");

  let expr = priceField.value === "0" ? "" : priceField.value;
  let lastSyncedPrice = priceField.value;

  function syncFromField(): void {
    if (priceField!.value !== lastSyncedPrice) {
      expr = priceField!.value === "0" ? "" : priceField!.value;
      lastSyncedPrice = priceField!.value;
    }
  }

  // Telegram's back button closes the sheet instead of closing the whole app,
  // and the MainButton hides while typing so a stray tap cannot submit a
  // half-typed amount.
  const backButton = ctx.tg?.BackButton;
  backButton?.onClick(closeNumpad);
  backdrop?.addEventListener("click", closeNumpad);

  numpadClear?.addEventListener("click", () => {
    expr = "";
    priceField.value = "";
    lastSyncedPrice = "";
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
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") closeNumpad();
  });

  function isOpen(): boolean {
    return !numpad!.classList.contains("hidden");
  }

  function openNumpad(): void {
    if (isOpen()) return;
    syncFromField();
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
    expr = priceField!.value === "0" ? "" : priceField!.value;
    lastSyncedPrice = priceField!.value;
    updateDisplay();
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
    if (numpadDisplay) {
      if (expr.includes("+")) {
        numpadDisplay.textContent = expr.endsWith("+")
          ? `${expr} (${Number(val).toLocaleString()})`
          : `${expr} = ${Number(val).toLocaleString()}`;
      } else {
        numpadDisplay.textContent = Number(val).toLocaleString();
      }
    }

    const currencyInput = document.getElementById("currency-input") as HTMLInputElement | null;
    const currency = currencyInput?.value ?? "TWD";

    const currencyLabel = document.getElementById("numpad-currency-label");
    if (currencyLabel) currencyLabel.textContent = currency;

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

  const rateInput = document.getElementById("exchange-rate-input") as HTMLInputElement | null;
  rateInput?.addEventListener("input", updateDisplay);

  const currencyInput = document.getElementById("currency-input") as HTMLInputElement | null;
  currencyInput?.addEventListener("change", updateDisplay);

  document.querySelectorAll("#currency-tabs [data-tui-tabs-trigger]").forEach((trigger) => {
    trigger.addEventListener("click", () => {
       setTimeout(updateDisplay, 10);
    });
  });

  document.querySelectorAll("[data-numpad-key]").forEach(btn => {
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      haptic(ctx, "impact", "light");
      syncFromField();
      const key = (btn as HTMLElement).dataset.numpadKey!;

      if (key === "backspace") {
        expr = expr.slice(0, -1);
      } else if (key === "+") {
        if (expr !== "" && !expr.endsWith("+")) {
          expr = expr + "+";
        }
      } else {
        const parts = expr.split("+");
        const last = parts[parts.length - 1] ?? "";
        if (key === "00" || key === "0") {
          if (last !== "" && last !== "0") {
            parts[parts.length - 1] = last + key;
          }
        } else {
          parts[parts.length - 1] = (last === "0" ? "" : last) + key;
        }
        expr = parts.join("+");
      }

      const terms = expr.split("+").filter((t) => t.length > 0);
      const total = terms.reduce((sum, t) => sum + parseInt(t, 10), 0);
      priceField!.value = total > 0 ? String(total) : "";
      lastSyncedPrice = priceField!.value;
      updateDisplay();
    });
  });

  numpadConfirm?.addEventListener("click", closeNumpad);
}
