import type { TelegramContext } from "./telegram.js";

export function apiUrl(
  path: string,
  ctx: TelegramContext,
  params: Record<string, string> = {},
): string {
  const url = new URL(path, window.location.origin);
  if (ctx.initData) url.searchParams.set("init_data", ctx.initData);
  for (const [key, value] of Object.entries(params)) {
    url.searchParams.set(key, value);
  }
  return url.pathname + url.search;
}

const SYMBOLS: Record<string, string> = { TWD: "NT$", JPY: "¥" };

export function symbol(currency: string): string {
  return SYMBOLS[currency] ?? `${currency} `;
}

export function tripCurrency(): string {
  return document.body.dataset.tripCurrency ?? "";
}

export function tripId(): string {
  return document.body.dataset.tripId ?? "";
}

export function appReady(): boolean {
  return document.body.dataset.appReady === "true" && tripId() !== "";
}
