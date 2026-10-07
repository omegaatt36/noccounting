/// <reference path="./telegram.d.ts" />

import { appReady } from "./api.js";

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
  // A long form should not close on a stray swipe, and losing a half-filled one
  // to closing the app deserves a confirmation. The header and background blend
  // into the Flexoki black page instead of Telegram's default color.
  tg.disableVerticalSwipes?.();
  tg.enableClosingConfirmation?.();
  tg.setHeaderColor?.("#100F0F");
  tg.setBackgroundColor?.("#100F0F");

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
  if (appReady()) {
    btn.show();
  } else {
    btn.hide();
    btn.disable();
  }
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
    if (appReady()) {
      ctx.tg.MainButton.enable();
    } else {
      ctx.tg.MainButton.hide();
      ctx.tg.MainButton.disable();
    }
  }
}
