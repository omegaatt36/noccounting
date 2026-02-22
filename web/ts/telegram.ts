/// <reference path="./telegram.d.ts" />

export interface TelegramContext {
  tg: TelegramWebApp | null;
  currentUserId: number | null;
  initData: string;
}

export function initTelegram(devMode: boolean): TelegramContext {
  if (devMode || !window.Telegram?.WebApp) {
    return { tg: null, currentUserId: null, initData: "" };
  }

  const tg = window.Telegram.WebApp;
  tg.ready();
  tg.expand();

  const initData = tg.initData || "";
  const currentUserId = tg.initDataUnsafe?.user?.id ?? null;

  return { tg, currentUserId, initData };
}

export function haptic(
  ctx: TelegramContext,
  type: "impact" | "notification",
  style?: string,
): void {
  if (!ctx.tg?.HapticFeedback) return;
  if (type === "impact") {
    ctx.tg.HapticFeedback.impactOccurred(
      (style as "light" | "medium" | "heavy") || "light",
    );
  } else if (type === "notification") {
    ctx.tg.HapticFeedback.notificationOccurred(
      (style as "error" | "success" | "warning") || "success",
    );
  }
}
