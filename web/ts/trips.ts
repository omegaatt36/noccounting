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

interface TripsResponse {
  current: number;
  trips: Trip[];
}

interface Member {
  id: string;
  name: string;
}

function setTrip(trip: Trip): void {
  document.body.dataset.tripId = String(trip.id);
  document.body.dataset.tripCurrency = trip.currency;
  const input = $("trip-id-input") as HTMLInputElement | null;
  if (input) input.value = String(trip.id);
}

// The empty result is a normal onboarding state — the bot's TREK account has
// not been added to any trip yet — and must not read as a failure.
export type TripsResult = "ok" | "empty" | "error";

export async function loadTrips(ctx: TelegramContext): Promise<TripsResult> {
  const select = $("trip-select") as HTMLSelectElement | null;
  if (!select) return "error";

  try {
    const res = await fetch(apiUrl("/api/trips", ctx));
    if (!res.ok) throw new Error(`Trip request failed: ${res.status}`);
    const data: TripsResponse = await res.json();
    select.textContent = "";

    if (data.trips.length === 0) {
      return "empty";
    }

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
        // A transient failure keeps the app open: revert the picker and say so.
        select.value = previousId;
        select.disabled = false;
        showView("app");
        showToast("⚠️ 切換失敗", "切換旅行失敗，請稍後再試");
        console.error("Failed to select trip:", error);
      }
    };
    return "ok";
  } catch (e) {
    console.error("Failed to load trips:", e);
    return "error";
  }
}

export async function loadMembers(ctx: TelegramContext, tripId: string): Promise<void> {
  const box = $("participants");
  if (!box) return;

  try {
    const res = await fetch(apiUrl("/api/members", ctx, { trip_id: tripId }));
    const data: { members: Member[] } = await res.json();
    box.textContent = "";

    for (const member of data.members) {
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
        "peer-checked:bg-primary peer-checked:text-primary-foreground peer-checked:border-primary transition-colors";
      chip.textContent = member.name;

      label.append(checkbox, chip);
      box.appendChild(label);
    }
  } catch (e) {
    console.error("Failed to load members:", e);
  }
}
