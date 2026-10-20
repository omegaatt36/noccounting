import { apiUrl } from "./api.js";
import { showView } from "./auth.js";
import { showToast } from "./toast.js";
import type { TelegramContext } from "./telegram.js";

const $ = (id: string) => document.getElementById(id);

interface Trip {
  id: number;
  title: string;
  label: string;
  currency: string;
}

interface Member {
  id: string;
  name: string;
}

interface User {
  telegram_id: number;
  nickname: string;
}

interface Bootstrap {
  users: User[];
  trips: Trip[];
  current: number;
  members: Member[];
}

function setTrip(trip: Trip): void {
  document.body.dataset.tripId = String(trip.id);
  document.body.dataset.tripCurrency = trip.currency;
  const input = $("trip-id-input") as HTMLInputElement | null;
  if (input) input.value = String(trip.id);
}

// The empty result is a normal onboarding state — the bot's TREK account has
// not been added to any trip yet — and must not read as a failure. "forbidden"
// is someone the server does not let in.
type BootstrapResult = "ok" | "forbidden" | "empty" | "error";

export async function loadBootstrap(ctx: TelegramContext): Promise<BootstrapResult> {
  try {
    const res = await fetch(apiUrl("/api/bootstrap", ctx));
    if (res.status === 403) return "forbidden";
    if (!res.ok) throw new Error(`Bootstrap request failed: ${res.status}`);
    const data: Bootstrap = await res.json();

    renderUsers(ctx, data.users);
    if (data.trips.length === 0) return "empty";
    renderTrips(ctx, data);
    renderMembers(data.members);
    return "ok";
  } catch (e) {
    console.error("Failed to load the app:", e);
    return "error";
  }
}

function renderUsers(ctx: TelegramContext, users: User[]): void {
  const select = $("paid-by-select") as HTMLSelectElement | null;
  if (!select) return;

  select.textContent = "";
  for (const u of users) {
    const opt = document.createElement("option");
    opt.value = String(u.telegram_id);
    const isCurrent = u.telegram_id === ctx.currentUserId;
    opt.textContent = isCurrent ? `${u.nickname} (本人)` : u.nickname;
    if (isCurrent) opt.selected = true;
    select.appendChild(opt);
  }
}

function renderTrips(ctx: TelegramContext, data: Bootstrap): void {
  const select = $("trip-select") as HTMLSelectElement | null;
  if (!select) return;

  select.textContent = "";
  for (const trip of data.trips) {
    const option = document.createElement("option");
    option.value = String(trip.id);
    option.textContent = `${trip.label}（${trip.currency}）`;
    option.selected = trip.id === data.current;
    select.appendChild(option);
  }
  setTrip(data.trips.find((trip) => trip.id === data.current) ?? data.trips[0]);

  select.onchange = async () => {
    const previousId = document.body.dataset.tripId ?? "";
    select.disabled = true;
    showView("loading");
    try {
      const response = await fetch(apiUrl("/api/trip", ctx), {
        method: "POST",
        body: new URLSearchParams({ trip_id: select.value }),
      });
      if (!response.ok) throw new Error(`Trip selection failed: ${response.status}`);
      window.location.reload();
    } catch (error) {
      select.value = previousId;
      select.disabled = false;
      showView("app");
      showToast("切換失敗", "切換旅行失敗，請稍後再試");
      console.error("Failed to select trip:", error);
    }
  };
}

function renderMembers(members: Member[]): void {
  const payerFilter = $("list-payer") as HTMLSelectElement | null;
  if (payerFilter?.appendChild) {
    payerFilter.textContent = "";
    const allOpt = document.createElement("option");
    allOpt.value = "";
    allOpt.textContent = "全部付款人";
    payerFilter.appendChild(allOpt);
    for (const member of members) {
      const opt = document.createElement("option");
      opt.value = member.id;
      opt.textContent = member.name;
      payerFilter.appendChild(opt);
    }
  }

  const box = $("participants");
  if (!box) return;

  box.textContent = "";
  for (const member of members) {
    const label = document.createElement("label");
    label.className = "cursor-pointer";

    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.name = "participants";
    checkbox.value = member.id;
    checkbox.checked = true;
    checkbox.className = "peer sr-only";

    const chip = document.createElement("span");
    chip.className =
      "inline-block px-3 py-1 rounded-full text-sm border border-input bg-muted text-muted-foreground " +
      "peer-checked:bg-primary peer-checked:text-primary-foreground peer-checked:border-primary " +
      "peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-ring transition-colors";
    chip.textContent = member.name;

    label.append(checkbox, chip);
    box.appendChild(label);
  }
}
