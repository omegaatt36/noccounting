const $ = (id: string) => document.getElementById(id);

type View = "form" | "dashboard";

export function switchView(view: View): void {
  const formView = $("view-form");
  const dashboardView = $("view-dashboard");
  const navForm = $("nav-form");
  const navDashboard = $("nav-dashboard");

  if (!formView || !dashboardView || !navForm || !navDashboard) return;

  if (view === "form") {
    formView.classList.remove("hidden");
    dashboardView.classList.add("hidden");
    navForm.className = "flex-1 py-3 text-center text-sm font-medium text-primary border-t-2 border-primary";
    navDashboard.className = "flex-1 py-3 text-center text-sm font-medium text-muted-foreground border-t-2 border-transparent";
  } else {
    formView.classList.add("hidden");
    dashboardView.classList.remove("hidden");
    navForm.className = "flex-1 py-3 text-center text-sm font-medium text-muted-foreground border-t-2 border-transparent";
    navDashboard.className = "flex-1 py-3 text-center text-sm font-medium text-primary border-t-2 border-primary";
  }
}

// Expose to global scope for onclick handlers in nav.templ
(window as any).switchView = switchView;
