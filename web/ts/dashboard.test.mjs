import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";

const template = await readFile("internal/app/webapp/components/dashboard.templ", "utf8");
const beforeHandlers = [...template.matchAll(/beforeRequest := fmt.Sprintf\("([^"\n]+)"/g)];
const completionHandlers = [...template.matchAll(/afterRequest := "([^"\n]+)"/g)];
const bindings = [...template.matchAll(/templ.Attributes\{([^}\n]+)\}/g)];

for (const [index, section] of ["category", "payment method"].entries()) {
  test(`htmx 4 ${section} details toggle and report completion`, () => {
    assert.match(bindings[index][1], /"hx-on:htmx:before:request": beforeRequest/);
    assert.match(bindings[index][1], /"hx-on:htmx:finally:request": afterRequest/);
    const chevron = { textContent: "▶" };
    const detail = { children: [], innerHTML: "" };
    const context = vm.createContext({
      document: { getElementById: () => detail },
      source: { querySelector: () => chevron },
      event: { preventDefault() { this.cancelled = true; } },
    });
    const run = (code) => vm.runInContext(`(function () { ${code} }).call(source)`, context);
    run(beforeHandlers[index][1]);
    assert.equal(chevron.textContent, "⏳");
    context.event.detail = { ctx: { response: { status: 200 } } };
    run(completionHandlers[index][1]);
    assert.equal(chevron.textContent, "▼");
    for (const response of [undefined, { status: 500 }]) {
      context.event.detail = { ctx: { response } };
      run(completionHandlers[index][1]);
      assert.equal(chevron.textContent, "▶");
    }
    detail.children = [{}];
    detail.innerHTML = "loaded details";
    run(beforeHandlers[index][1]);
    assert.equal(detail.innerHTML, "");
    assert.equal(chevron.textContent, "▶");
    assert.equal(context.event.cancelled, true);
  });
}
