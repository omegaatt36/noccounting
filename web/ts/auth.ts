const $ = (id: string) => document.getElementById(id);

export type ViewName = "loading" | "forbidden" | "trip-error" | "trip-empty" | "app";

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

export { showView };
