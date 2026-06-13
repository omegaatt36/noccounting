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

if (!ctx.tg?.MainButton) {
  document.getElementById("submit-btn")?.classList.remove("hidden");
}

// Set init_data hidden field
const initDataInput = document.getElementById(
  "init_data",
) as HTMLInputElement | null;
if (initDataInput) initDataInput.value = ctx.initData;

// Automatically attach init_data to all HTMX requests in non-dev mode
if (!DEV_MODE && ctx.initData) {
  document.body.addEventListener("htmx:configRequest", (evt) => {
    const htmxEvt = evt as CustomEvent<{ path: string }>;
    const path = htmxEvt.detail.path;
    if (path.includes("init_data=")) return;
    const sep = path.includes("?") ? "&" : "?";
    htmxEvt.detail.path = `${path}${sep}init_data=${encodeURIComponent(ctx.initData)}`;
  });
}

// Intercept CSV export links to append init_data
if (!DEV_MODE && ctx.initData) {
  document.addEventListener("click", (e) => {
    const link = (e.target as HTMLElement).closest(
      'a[href*="/api/export/csv"]',
    ) as HTMLAnchorElement | null;
    if (!link) return;
    e.preventDefault();
    const url = new URL(link.href, window.location.origin);
    url.searchParams.set("init_data", ctx.initData);
    const a = document.createElement("a");
    a.href = url.toString();
    a.download = link.download || "";
    a.style.display = "none";
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  });
}

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
