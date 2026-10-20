import { invalidate, switchView } from "./navigation.js";

const $ = (id: string) => document.getElementById(id);

const SEARCH_DELAY_MS = 300;

let searching: ReturnType<typeof setTimeout> | undefined;
document.addEventListener("input", (evt) => {
  if (!(evt.target instanceof Element)) return;
  if (evt.target.matches('#list-filters input[name="range"]')) {
    const dateField = $("list-date") as HTMLInputElement | null;
    if (dateField) dateField.value = "";
    return;
  }
  if (evt.target.id !== "list-q") return;
  clearTimeout(searching);
  searching = setTimeout(() => {
    $("list-filters")?.dispatchEvent(new CustomEvent("list-load"));
  }, SEARCH_DELAY_MS);
});

function showFiltered(filters: {
  category?: string;
  method?: string;
  payer?: string;
  date?: string;
}): void {
  const range = $("dashboard-content")?.dataset.range || "all";

  const set = (id: string, value: string) => {
    const field = $(id) as HTMLInputElement | HTMLSelectElement | null;
    if (field) field.value = value;
  };
  set("list-q", "");
  set("list-category", filters.category ?? "");
  set("list-method", filters.method ?? "");
  set("list-payer", filters.payer ?? "");
  set("list-date", filters.date ?? "");
  const chip = document.querySelector<HTMLInputElement>(
    `#list-filters input[name="range"][value="${range}"]`,
  );
  if (chip) chip.checked = true;

  invalidate("list");
  switchView("list");
}

document.addEventListener("click", (evt) => {
  const target = evt.target instanceof Element ? evt.target : null;
  const category = target?.closest<HTMLElement>("[data-list-category]")?.dataset.listCategory;
  const method = target?.closest<HTMLElement>("[data-list-method]")?.dataset.listMethod;
  const payer = target?.closest<HTMLElement>("[data-list-payer]")?.dataset.listPayer;
  const date = target?.closest<HTMLElement>("[data-list-date]")?.dataset.listDate;
  if (category) showFiltered({ category });
  else if (method) showFiltered({ method });
  else if (payer) showFiltered({ payer });
  else if (date) showFiltered({ date });
});
