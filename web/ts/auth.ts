import type { TelegramContext } from "./telegram.js";

const $ = (id: string) => document.getElementById(id);

type ViewName = "loading" | "forbidden" | "trip-error" | "trip-empty" | "app";

function showView(view: ViewName): void {
  const ready = view === "app";
  document.body.dataset.appReady = String(ready);
  const mainButton = window.Telegram?.WebApp?.MainButton;
  if (ready) {
    mainButton?.show();
    mainButton?.enable();
  } else {
    mainButton?.hide();
    mainButton?.disable();
  }

  const loading = $("loading");
  const forbidden = $("forbidden");
  const app = $("app");
  const tripError = $("trip-error");
  const tripEmpty = $("trip-empty");
  if (tripError) tripError.classList.toggle("hidden", view !== "trip-error");
  if (tripEmpty) tripEmpty.classList.toggle("hidden", view !== "trip-empty");

  if (loading) {
    loading.className =
      view === "loading"
        ? "flex flex-col items-center justify-center min-h-screen"
        : "hidden";
  }
  if (forbidden) {
    forbidden.className =
      view === "forbidden"
        ? "flex flex-col items-center justify-center min-h-screen"
        : "hidden";
  }
  if (app) {
    // pb-20 clears the fixed bottom nav; dropping it hides the form tail under it.
    app.className = view === "app" ? "max-w-md mx-auto px-4 py-6 pb-20" : "hidden";
  }
}

async function loadUsers(
  ctx: TelegramContext,
  devMode: boolean,
): Promise<void> {
  try {
    const url = devMode
      ? "/api/users"
      : `/api/users?init_data=${encodeURIComponent(ctx.initData)}`;
    const res = await fetch(url);
    const data = await res.json();
    const select = $("paid-by-select") as HTMLSelectElement | null;
    if (!select) return;

    select.textContent = "";

    data.users.forEach((u: { telegram_id: number; nickname: string }) => {
      const opt = document.createElement("option");
      opt.value = String(u.telegram_id);
      const isCurrent = u.telegram_id === ctx.currentUserId;
      opt.textContent = isCurrent ? `${u.nickname} (本人)` : u.nickname;
      if (isCurrent) opt.selected = true;
      select.appendChild(opt);
    });
  } catch (e) {
    console.error("Failed to load users:", e);
  }
}

export { showView };

export async function authenticate(
  ctx: TelegramContext,
  devMode: boolean,
): Promise<boolean> {
  showView("loading");

  if (devMode) {
    await loadUsers(ctx, devMode);
    return true;
  }

  if (!ctx.initData) {
    showView("forbidden");
    return false;
  }

  try {
    const res = await fetch(
      `/api/auth?init_data=${encodeURIComponent(ctx.initData)}`,
    );
    const data = await res.json();
    if (!data.authorized) {
      showView("forbidden");
      return false;
    }
    await loadUsers(ctx, devMode);
    return true;
  } catch (e) {
    console.error("Auth error:", e);
    showView("forbidden");
    return false;
  }
}
