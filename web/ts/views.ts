import { registerLoader, reloadView } from "./navigation.js";
import type { DataView } from "./navigation.js";

const $ = (id: string) => document.getElementById(id);

// htmx events fire on the element that made the request. One that came from
// inside a swapped region may already be gone by `finally`, in which case the
// event never reaches `document`; the handlers below only rely on `before`
// (always attached) and on `finally` for requests whose response was not swapped.
function sourceOf(evt: Event): Element | null {
  return evt.target instanceof Element ? evt.target : null;
}

function statusOf(evt: Event): number {
  const detail = (evt as CustomEvent<{ ctx?: { response?: { status: number } } }>).detail;
  return detail?.ctx?.response?.status ?? 0;
}

function succeeded(status: number): boolean {
  return status >= 200 && status < 300;
}

// The data view a request is drawing, named by the element that makes it.
function viewOf(evt: Event): string | undefined {
  return sourceOf(evt)?.closest<HTMLElement>("[data-view-source]")?.dataset.viewSource;
}

document.addEventListener("htmx:before:request", (evt) => {
  const view = viewOf(evt);
  if (view) $(`${view}-error`)?.classList.add("hidden");
});

// The failed load was not swapped in, so the previous content (or the skeleton)
// is still on screen; say why and offer another try.
document.addEventListener("htmx:finally:request", (evt) => {
  const view = viewOf(evt);
  if (view && !succeeded(statusOf(evt))) $(`${view}-error`)?.classList.remove("hidden");
});

registerLoader("list", () => {
  $("list-filters")?.dispatchEvent(new CustomEvent("list-load"));
});

registerLoader("settle", () => {
  $("settle-host")?.dispatchEvent(new CustomEvent("settle-load"));
});

// The statistics are drawn for a period. Redrawing takes the one showing; a
// view never drawn has no period yet and loads the default.
registerLoader("stats", () => {
  const active = $("stats-host")?.querySelector<HTMLButtonElement>(
    '[data-range][aria-pressed="true"]',
  );
  if (active) active.click();
  else $("stats-host")?.dispatchEvent(new CustomEvent("stats-load"));
});

document.addEventListener("click", (evt) => {
  const target = evt.target instanceof Element ? evt.target : null;
  const retry = target?.closest<HTMLElement>('[data-action="retry-view"]');
  if (retry) reloadView(retry.dataset.view as DataView);
});
