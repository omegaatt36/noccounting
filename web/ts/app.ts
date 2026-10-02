import { initTelegram } from "./telegram.js";
import { authenticate, showView } from "./auth.js";
import { restoreDefaults } from "./storage.js";
import { setupEventListeners } from "./form.js";
import {
  updateExchangeRateVisibility,
  fetchExchangeRate,
} from "./exchange-rate.js";
import { setupNumpad } from "./numpad.js";
import { loadTrips, loadMembers } from "./trips.js";
import { tripCurrency, tripId } from "./api.js";
import "./navigation.js";

const DEV_MODE = !!document.getElementById("dev-mode-flag");
const ctx = initTelegram(DEV_MODE);

if (!ctx.tg?.MainButton) {
  document.getElementById("submit-btn")?.classList.remove("hidden");
}

const initDataInput = document.getElementById(
  "init_data",
) as HTMLInputElement | null;
if (initDataInput) initDataInput.value = ctx.initData;

// Every HTMX request is about the trip the page was opened for, and in non-dev
// mode carries the init_data that says who is asking.
document.body.addEventListener("htmx:configRequest", (evt) => {
  const htmxEvt = evt as CustomEvent<{ path: string }>;
  let path = htmxEvt.detail.path;
  const add = (key: string, value: string) => {
    if (!value || path.includes(`${key}=`)) return;
    path = `${path}${path.includes("?") ? "&" : "?"}${key}=${encodeURIComponent(value)}`;
  };
  if (!DEV_MODE) add("init_data", ctx.initData);
  add("trip_id", tripId());
  htmxEvt.detail.path = path;
});

{
  document.addEventListener("click", (e) => {
    const link = (e.target as HTMLElement).closest(
      'a[href*="/api/export/csv"]',
    ) as HTMLAnchorElement | null;
    if (!link) return;
    e.preventDefault();
    const url = new URL(link.href, window.location.origin);
    if (!DEV_MODE && ctx.initData) url.searchParams.set("init_data", ctx.initData);
    url.searchParams.set("trip_id", tripId());
    const a = document.createElement("a");
    a.href = url.toString();
    a.download = link.download || "";
    a.style.display = "none";
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  });
}

authenticate(ctx, DEV_MODE).then(async (authorized) => {
  if (!authorized) return;
  if (!(await loadTrips(ctx))) {
    showView("trip-error");
    return;
  }
  await loadMembers(ctx, tripId());
  setupEventListeners(ctx);
  setupNumpad(ctx);
  restoreDefaults(updateExchangeRateVisibility);

  const currencyInput = document.getElementById(
    "currency-input",
  ) as HTMLInputElement | null;
  if (currencyInput && currencyInput.value !== tripCurrency()) {
    fetchExchangeRate(ctx);
  }
  showView("app");
});
