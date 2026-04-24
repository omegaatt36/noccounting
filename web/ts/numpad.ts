import type { TelegramContext } from "./telegram.js";
import { haptic } from "./telegram.js";

export function setupNumpad(ctx: TelegramContext): void {
  const priceField = document.getElementById("price") as HTMLInputElement | null;
  const numpad = document.getElementById("numpad-sheet");
  const numpadDisplay = document.getElementById("numpad-display");
  const numpadConvert = document.getElementById("numpad-convert");
  const numpadConfirm = document.getElementById("numpad-confirm");

  if (!priceField || !numpad) return;

  // Prevent system keyboard
  priceField.setAttribute("inputmode", "none");
  priceField.setAttribute("readonly", "true");

  priceField.addEventListener("click", () => openNumpad());

  function openNumpad(): void {
    numpad!.classList.remove("hidden");
    numpad!.classList.add("flex");
    updateDisplay();
  }

  function closeNumpad(): void {
    numpad!.classList.add("hidden");
    numpad!.classList.remove("flex");
  }

  function updateDisplay(): void {
    const val = priceField!.value || "0";
    if (numpadDisplay) numpadDisplay.textContent = Number(val).toLocaleString();

    const currencyInput = document.getElementById("currency-input") as HTMLInputElement | null;
    const currency = currencyInput?.value ?? "TWD";

    // Update currency label
    const currencyLabel = document.getElementById("numpad-currency-label");
    if (currencyLabel) currencyLabel.textContent = currency;

    // Live TWD conversion
    const rateInput = document.getElementById("exchange-rate-input") as HTMLInputElement | null;
    if (numpadConvert && currency === "JPY" && rateInput) {
      const twd = Math.round(parseFloat(val) * parseFloat(rateInput.value || "0.22"));
      numpadConvert.textContent = isNaN(twd) ? "" : `≈ NT$ ${twd.toLocaleString()}`;
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
