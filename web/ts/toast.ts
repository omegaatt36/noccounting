// Mirrors the markup of the server-rendered toast (components/ui/toast) for
// errors that never go through HTMX, so a failed trip switch can say so without
// leaving the app.
const $ = (id: string) => document.getElementById(id);

const DURATION_MS = 5000;

export function showToast(title: string, description: string): void {
  const host = $("result");
  if (!host) return;

  const toast = document.createElement("div");
  toast.setAttribute("data-tui-toast", "");
  toast.setAttribute("data-tui-toast-duration", String(DURATION_MS));
  toast.setAttribute("data-position", "top-center");
  toast.setAttribute("data-variant", "error");
  toast.className =
    "z-50 fixed pointer-events-auto p-4 w-full md:max-w-[420px] top-0 left-1/2 -translate-x-1/2";

  const box = document.createElement("div");
  box.className =
    "w-full bg-popover text-popover-foreground rounded-lg shadow-xs border pt-5 pb-4 px-4 flex items-center justify-center relative overflow-hidden group";

  const content = document.createElement("span");
  content.className = "flex-1 min-w-0";
  const titleEl = document.createElement("p");
  titleEl.className = "text-sm font-semibold truncate";
  titleEl.textContent = title;
  const descriptionEl = document.createElement("p");
  descriptionEl.className = "text-sm opacity-90 mt-1";
  descriptionEl.textContent = description;
  content.append(titleEl, descriptionEl);
  box.appendChild(content);
  toast.appendChild(box);
  host.appendChild(toast);

  setTimeout(() => {
    toast.style.transition = "opacity 300ms, transform 300ms";
    toast.style.opacity = "0";
    toast.style.transform = "translateY(-1rem)";
    setTimeout(() => toast.remove(), 300);
  }, DURATION_MS);
}
