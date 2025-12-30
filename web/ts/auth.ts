import type { TelegramContext } from "./telegram.js";
import { STORAGE_KEYS } from "./storage.js";

const $ = (id: string) => document.getElementById(id);

type ViewName = "loading" | "forbidden" | "app";

function showView(view: ViewName): void {
  const loading = $("loading");
  const forbidden = $("forbidden");
  const app = $("app");

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
    app.className = view === "app" ? "max-w-md mx-auto px-4 py-6" : "hidden";
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

    const savedPaidBy = localStorage.getItem(STORAGE_KEYS.paidBy);

    data.users.forEach((u: { telegram_id: number; nickname: string }) => {
      const opt = document.createElement("option");
      opt.value = String(u.telegram_id);
      const isCurrent = u.telegram_id === ctx.currentUserId;
      opt.textContent = isCurrent ? `${u.nickname} (本人)` : u.nickname;

      if (savedPaidBy && String(u.telegram_id) === savedPaidBy) {
        opt.selected = true;
      } else if (!savedPaidBy && isCurrent) {
        opt.selected = true;
      }
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
): Promise<void> {
  showView("loading");

  if (devMode) {
    await loadUsers(ctx, devMode);
    showView("app");
    return;
  }

  if (!ctx.initData) {
    showView("forbidden");
    return;
  }

  try {
    const res = await fetch(
      `/api/auth?init_data=${encodeURIComponent(ctx.initData)}`,
    );
    const data = await res.json();
    if (!data.authorized) {
      showView("forbidden");
      return;
    }
    await loadUsers(ctx, devMode);
    showView("app");
  } catch (e) {
    console.error("Auth error:", e);
    showView("forbidden");
  }
}
