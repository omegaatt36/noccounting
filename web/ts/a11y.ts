// The category, currency and payment-method pickers are templui tabs whose selected one carries
// data-active. They are single-choice groups, so announce them as radios and keep aria-checked in step.
const CHOICE_GROUPS = ["category-tabs", "currency-tabs", "method-tabs"];

export function setupChoiceGroups(): void {
  for (const id of CHOICE_GROUPS) {
    const group = document.getElementById(id);
    if (!group) continue;

    const triggers = group.querySelectorAll<HTMLElement>("[data-slot=tabs-trigger]");
    group.querySelectorAll<HTMLElement>("[data-slot=tabs-list]").forEach((list) => list.setAttribute("role", "radiogroup"));
    const sync = () => {
      for (const trigger of triggers) {
        trigger.setAttribute("aria-checked", String(trigger.hasAttribute("data-active")));
        trigger.removeAttribute("aria-selected");
        trigger.removeAttribute("aria-controls");
      }
    };

    for (const trigger of triggers) trigger.setAttribute("role", "radio");
    sync();
    new MutationObserver(sync).observe(group, {
      subtree: true,
      attributes: true,
      attributeFilter: ["data-active", "aria-selected", "aria-controls"],
    });
  }
}
