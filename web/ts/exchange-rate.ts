import { apiUrl, symbol, tripCurrency } from "./api.js";
import type { TelegramContext } from "./telegram.js";
import { STORAGE_KEYS } from "./storage.js";

const $ = (id: string) => document.getElementById(id);

interface Quote {
  from: string;
  to: string;
  rate: string;
}

function currentCurrency(): string {
  return ($("currency-input") as HTMLInputElement | null)?.value ?? "";
}

export function updateExchangeRateVisibility(): void {
  const section = $("exchange-rate-section");
  if (!section) return;

  const base = tripCurrency();
  const foreign = base !== "" && currentCurrency() !== base;
  section.style.maxHeight = foreign ? "120px" : "0";
  section.style.opacity = foreign ? "1" : "0";

  const label = section.querySelector("label");
  if (label && foreign) label.textContent = `1 ${currentCurrency()} =`;
}

export function loadCachedRate(): void {
  const key = cacheKey();
  const cached = localStorage.getItem(key);
  const cachedDate = localStorage.getItem(`${key}_date`);
  const input = $("exchange-rate-input") as HTMLInputElement | null;
  const status = $("rate-status");

  if (!input || !status) return;

  if (cached) {
    input.value = cached;
    status.textContent = `(快取 ${cachedDate || "?"})`;
  } else {
    status.textContent = "(自動)";
  }
}

// A rate is cached per direction: 0.215 TWD per JPY is not 0.215 JPY per TWD.
function cacheKey(): string {
  return `${STORAGE_KEYS.exchangeRate}_${currentCurrency()}_${tripCurrency()}`;
}

function showBothDirections(quotes: Quote[]): void {
  const hint = $("rate-hint");
  if (!hint) return;
  hint.textContent = quotes
    .map((q) => `1 ${q.from} = ${symbol(q.to)}${Number(q.rate).toFixed(4)}`)
    .join("　·　");
}

let inflight: Promise<void> | null = null;

export function fetchExchangeRate(ctx: TelegramContext): Promise<void> {
  // Restoring the saved currency and the first load both ask; one answer serves.
  inflight ??= requestExchangeRate(ctx).finally(() => {
    inflight = null;
  });
  return inflight;
}

async function requestExchangeRate(ctx: TelegramContext): Promise<void> {
  const btn = $("fetch-rate-btn") as HTMLButtonElement | null;
  const input = $("exchange-rate-input") as HTMLInputElement | null;
  const status = $("rate-status");

  if (!btn || !input || !status) return;

  btn.disabled = true;
  btn.querySelector(".fetch-text")?.classList.add("hidden");
  btn.querySelector(".fetch-loading")?.classList.remove("hidden");

  try {
    const res = await fetch(apiUrl("/api/rates", ctx));
    const data: { quotes: Quote[] } = await res.json();
    showBothDirections(data.quotes);

    const quote = data.quotes.find(
      (q) => q.from === currentCurrency() && q.to === tripCurrency(),
    );
    if (quote) {
      input.value = quote.rate;
      localStorage.setItem(cacheKey(), quote.rate);
      localStorage.setItem(`${cacheKey()}_date`, new Date().toISOString().slice(0, 10));
      status.textContent = "(即時)";
    } else {
      loadCachedRate();
    }
  } catch (e) {
    console.error("Exchange rate fetch failed:", e);
    loadCachedRate();
  } finally {
    btn.disabled = false;
    btn.querySelector(".fetch-text")?.classList.remove("hidden");
    btn.querySelector(".fetch-loading")?.classList.add("hidden");
  }
}
