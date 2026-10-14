import { initTelegram } from "./telegram.js";
import { showView } from "./auth.js";
import { restoreDefaults } from "./storage.js";
import { setupEventListeners } from "./form.js";
import { setupEdit } from "./edit.js";
import { isEditing } from "./fields.js";
import { updateExchangeRateVisibility } from "./exchange-rate.js";
import { setupNumpad } from "./numpad.js";
import { loadBootstrap } from "./trips.js";
import { tripId } from "./api.js";
import "./navigation.js";
import "./views.js";
import "./list.js";

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
// mode carries the init_data that says who is asking. The expense form files a
// new expense, or changes the one it was opened on.
document.body.addEventListener("htmx:config:request", (evt) => {
  const htmxEvt = evt as CustomEvent<{
    ctx: { request: { action: string; method: string; form?: Element } };
  }>;
  const request = htmxEvt.detail.ctx.request;
  // Not form.id: the form has a field named id, which shadows the attribute.
  if (request.form?.matches("#expense-form") && isEditing()) request.method = "PUT";
  let path = request.action;
  const add = (key: string, value: string) => {
    if (!value || path.includes(`${key}=`)) return;
    path = `${path}${path.includes("?") ? "&" : "?"}${key}=${encodeURIComponent(value)}`;
  };
  if (!DEV_MODE) add("init_data", ctx.initData);
  add("trip_id", tripId());
  request.action = path;
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

document.addEventListener("click", (e) => {
  if ((e.target as HTMLElement).closest('[data-action="reload"]')) {
    window.location.reload();
  }
});

const FAILED_VIEW = {
  forbidden: "forbidden",
  empty: "trip-empty",
  error: "trip-error",
} as const;

async function start(): Promise<void> {
  showView("loading");
  if (!DEV_MODE && !ctx.initData) {
    showView("forbidden");
    return;
  }

  const result = await loadBootstrap(ctx);
  if (result !== "ok") {
    showView(FAILED_VIEW[result]);
    return;
  }

  setupEventListeners(ctx);
  setupEdit(ctx);
  setupNumpad(ctx);
  await restoreDefaults(updateExchangeRateVisibility);
  showView("app");
}

void start();
