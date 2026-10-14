import { appReady } from "./api.js";

const $ = (id: string) => document.getElementById(id);

export type DataView = "list" | "stats" | "settle";
export type View = "form" | DataView;

const VIEWS: readonly View[] = ["form", "list", "stats", "settle"];
const DATA_VIEWS: readonly DataView[] = ["list", "stats", "settle"];

const ACTIVE = ["text-primary", "border-primary"];
const INACTIVE = ["text-muted-foreground", "border-transparent"];

// A data view draws itself from the server. It is drawn when first opened and
// again when opened after an expense changed, not on every visit.
const loaders = new Map<DataView, () => void>();
const stale = new Set<View>(DATA_VIEWS);

export function registerLoader(view: DataView, load: () => void): void {
  loaders.set(view, load);
}

// Draws a data view now.
export function reloadView(view: DataView): void {
  loaders.get(view)?.();
}

// Marks a data view as out of date; it is redrawn when next opened.
export function invalidate(view: DataView): void {
  stale.add(view);
}

// An expense that was filed, edited or deleted makes every view stale.
document.addEventListener("expense-saved", () => {
  for (const view of DATA_VIEWS) invalidate(view);
});

export function switchView(view: View): void {
  for (const name of VIEWS) {
    $(`view-${name}`)?.classList.toggle("hidden", name !== view);
  }

  for (const button of document.querySelectorAll<HTMLElement>("#bottom-nav [data-view]")) {
    const current = button.dataset.view === view;
    button.classList.remove(...(current ? INACTIVE : ACTIVE));
    button.classList.add(...(current ? ACTIVE : INACTIVE));
    if (current) button.setAttribute("aria-current", "page");
    else button.removeAttribute("aria-current");
  }

  // The MainButton files the form, which is not on screen elsewhere.
  const mainButton = window.Telegram?.WebApp?.MainButton;
  if (view === "form" && appReady()) mainButton?.show();
  else mainButton?.hide();

  window.scrollTo(0, 0);

  if (stale.has(view)) {
    stale.delete(view);
    reloadView(view as DataView);
  }
}

document.addEventListener("click", (evt) => {
  const target = evt.target instanceof Element ? evt.target : null;
  const view = target?.closest<HTMLElement>("[data-view]")?.dataset.view;
  if (view && (VIEWS as readonly string[]).includes(view) && target?.closest("#bottom-nav")) {
    switchView(view as View);
  } else if (target?.closest('[data-action="show-form"]')) {
    switchView("form");
  }
});
