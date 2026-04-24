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

export function setupMainButton(
  ctx: TelegramContext,
  onSubmit: () => void,
): void {
  if (!ctx.tg?.MainButton) return;

  const btn = ctx.tg.MainButton;
  btn.setText("✅ 新增消費");
  btn.color = "#4385BE"; // Flexoki blue
  btn.textColor = "#FFFFFF";
  btn.show();
  btn.onClick(onSubmit);
}

export function setMainButtonLoading(
  ctx: TelegramContext,
  loading: boolean,
): void {
  if (!ctx.tg?.MainButton) return;
  if (loading) {
    ctx.tg.MainButton.showProgress(false);
    ctx.tg.MainButton.disable();
  } else {
    ctx.tg.MainButton.hideProgress();
    ctx.tg.MainButton.enable();
  }
}
