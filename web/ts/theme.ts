// Loaded as a classic script in <head> so the page theme is on <html> before the body paints. Inside Telegram it follows
// Telegram's colorScheme live; elsewhere the OS preference. The colors come from the CSS tokens.
(() => {
  const root = document.documentElement;
  const tg = window.Telegram?.WebApp;
  const inTelegram = !!tg?.initData;
  const prefersDark = window.matchMedia("(prefers-color-scheme: dark)");

  const token = (name: string): string =>
    getComputedStyle(root).getPropertyValue(name).trim();

  function apply(): void {
    const scheme = inTelegram
      ? tg?.colorScheme === "light"
        ? "light"
        : "dark"
      : prefersDark.matches
        ? "dark"
        : "light";
    root.dataset.theme = scheme;
    if (!inTelegram || !tg) return;

    const background = token("--color-background");
    if (background) {
      tg.setHeaderColor?.(background);
      tg.setBackgroundColor?.(background);
    }
    tg.MainButton?.setParams({
      color: token("--color-primary"),
      text_color: token("--color-primary-foreground"),
    });
  }

  apply();
  if (inTelegram) {
    tg?.onEvent?.("themeChanged", apply);
  } else {
    prefersDark.addEventListener("change", apply);
  }
})();
