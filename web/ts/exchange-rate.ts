import { STORAGE_KEYS } from "./storage.js";

const $ = (id: string) => document.getElementById(id);

export function updateExchangeRateVisibility(): void {
  const section = $("exchange-rate-section");
  const currencyInput = $("currency-input") as HTMLInputElement | null;
  if (!section || !currencyInput) return;

  if (currencyInput.value === "JPY") {
    section.classList.remove("hidden");
  } else {
    section.classList.add("hidden");
  }
}

export function loadCachedRate(): void {
  const cached = localStorage.getItem(STORAGE_KEYS.exchangeRate);
  const cachedDate = localStorage.getItem(STORAGE_KEYS.exchangeRateDate);
  const input = $("exchange-rate-input") as HTMLInputElement | null;
  const status = $("rate-status");

  if (!input || !status) return;

  if (cached) {
    input.value = cached;
    status.textContent = `(快取 ${cachedDate || "?"})`;
  } else {
    status.textContent = "(預設)";
  }
}

export async function fetchExchangeRate(): Promise<void> {
  const btn = $("fetch-rate-btn") as HTMLButtonElement | null;
  const input = $("exchange-rate-input") as HTMLInputElement | null;
  const status = $("rate-status");

  if (!btn || !input || !status) return;

  btn.disabled = true;
  btn.querySelector(".fetch-text")?.classList.add("hidden");
  btn.querySelector(".fetch-loading")?.classList.remove("hidden");

  try {
    const yesterday = new Date();
    yesterday.setDate(yesterday.getDate() - 1);
    const startDate = yesterday.toISOString().split("T")[0];

    const res = await fetch(
      `https://api.finmindtrade.com/api/v4/data?dataset=TaiwanExchangeRate&data_id=JPY&start_date=${startDate}`
    );
    const data = await res.json();

    if (data.status === 200 && data.data && data.data.length > 0) {
      const rate = data.data[data.data.length - 1].cash_sell;
      input.value = rate;
      localStorage.setItem(STORAGE_KEYS.exchangeRate, rate);
      localStorage.setItem(STORAGE_KEYS.exchangeRateDate, new Date().toISOString().slice(0, 10));
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
