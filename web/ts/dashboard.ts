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

function setChevron(row: Element, symbol: string): void {
  const chevron = row.querySelector(".chevron");
  if (chevron) chevron.textContent = symbol;
}

// A category or payment-method row opens its expense list on the first click
// and empties it on the next, so the request only goes out when it is closed.
document.addEventListener("htmx:before:request", (evt) => {
  const row = sourceOf(evt)?.closest<HTMLElement>("[data-detail]");
  if (row) {
    const detail = $(row.dataset.detail ?? "");
    if (detail && detail.children.length > 0) {
      detail.innerHTML = "";
      row.setAttribute("aria-expanded", "false");
      setChevron(row, "▶");
      evt.preventDefault();
    } else {
      setChevron(row, "⏳");
    }
    return;
  }

  if (sourceOf(evt)?.matches("[data-dashboard-load]")) {
    $("dashboard-error")?.classList.add("hidden");
  }
});

document.addEventListener("htmx:finally:request", (evt) => {
  const source = sourceOf(evt);
  const status = statusOf(evt);

  const row = source?.closest<HTMLElement>("[data-detail]");
  if (row) {
    const opened = succeeded(status);
    row.setAttribute("aria-expanded", String(opened));
    setChevron(row, opened ? "▼" : "▶");
    return;
  }

  // The failed load was not swapped in, so the previous content (or the
  // skeleton) is still on screen; say why and offer another try.
  if (source?.matches("[data-dashboard-load]") && !succeeded(status)) {
    $("dashboard-error")?.classList.remove("hidden");
  }
});

function activeRange(): HTMLButtonElement | null {
  return $("dashboard-host")?.querySelector<HTMLButtonElement>(
    '[data-range][aria-pressed="true"]',
  ) ?? null;
}

// Redraws the dashboard in the range it is showing. A dashboard that was never
// opened has nothing to redraw: it loads, with fresh data, on its first view.
export function refreshDashboard(): void {
  activeRange()?.click();
}

function retryDashboard(): void {
  const active = activeRange();
  if (active) {
    active.click();
    return;
  }
  $("dashboard-host")?.dispatchEvent(new CustomEvent("dashboard-load"));
}

document.addEventListener("click", (evt) => {
  const target = evt.target instanceof Element ? evt.target : null;
  if (target?.closest('[data-action="retry-dashboard"]')) retryDashboard();
});

// A delete response names this event. htmx fires it after the swap, when the
// delete button is already off the page, so it lands on `document`; every card
// of the dashboard is stale by then.
document.addEventListener("dashboard-refresh", refreshDashboard);
