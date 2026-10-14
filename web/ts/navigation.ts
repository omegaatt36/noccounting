import { appReady } from "./api.js";
import { refreshDashboard } from "./dashboard.js";

const $ = (id: string) => document.getElementById(id);

type View = "form" | "dashboard";

const NAV_BASE = "flex-1 py-3 text-center text-sm font-medium border-t-2 ";
const NAV_ACTIVE = NAV_BASE + "text-primary border-primary";
const NAV_INACTIVE = NAV_BASE + "text-muted-foreground border-transparent";

// An expense filed after the dashboard was drawn makes it stale; it is redrawn
// when next opened rather than on every visit.
let dashboardStale = false;
document.addEventListener("expense-saved", () => {
  dashboardStale = true;
});

export function switchView(view: View): void {
  const formView = $("view-form");
  const dashboardView = $("view-dashboard");
  if (!formView || !dashboardView) return;

  formView.classList.toggle("hidden", view !== "form");
  dashboardView.classList.toggle("hidden", view !== "dashboard");

  for (const button of document.querySelectorAll<HTMLElement>("#bottom-nav [data-view]")) {
    const current = button.dataset.view === view;
    button.className = current ? NAV_ACTIVE : NAV_INACTIVE;
    if (current) button.setAttribute("aria-current", "page");
    else button.removeAttribute("aria-current");
  }

  // The MainButton files the form, which is not on screen in the dashboard.
  const mainButton = window.Telegram?.WebApp?.MainButton;
  if (view === "form" && appReady()) mainButton?.show();
  else mainButton?.hide();

  window.scrollTo(0, 0);

  if (view === "dashboard" && dashboardStale) {
    dashboardStale = false;
    refreshDashboard();
  }
}

document.addEventListener("click", (evt) => {
  const target = evt.target instanceof Element ? evt.target : null;
  const view = target?.closest<HTMLElement>("[data-view]")?.dataset.view;
  if (view === "form" || view === "dashboard") switchView(view);
  else if (target?.closest('[data-action="show-form"]')) switchView("form");
});
