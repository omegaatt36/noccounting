// The category, currency and payment-method pickers are templui tabs whose selected one carries
// data-tui-tabs-state="active". They are single-choice groups, so announce them as radios and keep aria-checked in step.
const CHOICE_GROUPS = ["category-tabs", "currency-tabs", "method-tabs"];

export function setupChoiceGroups(): void {
  for (const id of CHOICE_GROUPS) {
    const group = document.getElementById(id);
    if (!group) continue;

    const triggers = group.querySelectorAll<HTMLElement>("[data-tui-tabs-trigger]");
    const sync = () => {
      for (const trigger of triggers) {
        trigger.setAttribute("aria-checked", String(trigger.dataset.tuiTabsState === "active"));
      }
    };

    for (const trigger of triggers) trigger.setAttribute("role", "radio");
    sync();
    new MutationObserver(sync).observe(group, {
      subtree: true,
      attributes: true,
      attributeFilter: ["data-tui-tabs-state"],
    });
  }
}
