// Request a toast through the same marker used by server-rendered responses.
export function showToast(
  title: string,
  description: string,
  variant: "error" | "success" = "error",
): void {
  const host = document.getElementById("result");
  if (!host) return;

  const toast = document.createElement("div");
  toast.setAttribute("data-templ-toast", "");
  toast.setAttribute("data-templ-title", title);
  toast.setAttribute("data-templ-description", description);
  toast.setAttribute("data-templ-type", variant);
  host.appendChild(toast);
}
