import { initTelegram } from "./telegram.js";
import { authenticate } from "./auth.js";
import { restoreDefaults } from "./storage.js";
import { setupEventListeners } from "./form.js";
import {
  updateExchangeRateVisibility,
  fetchExchangeRate,
} from "./exchange-rate.js";
import { setupNumpad } from "./numpad.js";
import "./navigation.js";

const DEV_MODE = !!document.getElementById("dev-mode-flag");
const ctx = initTelegram(DEV_MODE);

// Set init_data hidden field
const initDataInput = document.getElementById(
  "init_data",
) as HTMLInputElement | null;
if (initDataInput) initDataInput.value = ctx.initData;

// Setup all event listeners
setupEventListeners(ctx);
setupNumpad(ctx);

// Authenticate and initialize
authenticate(ctx, DEV_MODE).then(() => {
  restoreDefaults(updateExchangeRateVisibility);

  const currencyInput = document.getElementById(
    "currency-input",
  ) as HTMLInputElement | null;
  if (currencyInput?.value === "JPY") {
    fetchExchangeRate();
  }
});
