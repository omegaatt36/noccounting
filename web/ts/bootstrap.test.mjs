import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";

async function bootstrap(result, options = {}) {
  const calls = [];
  const context = vm.createContext({
    document: {
      getElementById: (id) => id === "dev-mode-flag" && options.devMode ? {} : null,
      body: { addEventListener(name, fn) { if (options.handlers) options.handlers[name] = fn; } },
      addEventListener() {},
    },
  });
  const functions = {
    initTelegram: () => ({ initData: options.initData ?? "" }),
    showView: (view) => calls.push(view),
    loadBootstrap: async () => { calls.push("bootstrap"); return await result; },
    setupEventListeners: () => calls.push("form"),
    setupEdit: () => calls.push("edit"),
    setupNumpad: () => calls.push("numpad"),
    restoreDefaults() {}, updateExchangeRateVisibility() {}, fetchExchangeRate() {},
    isEditing: () => options.editing ?? false,
    tripId: () => "3", tripCurrency: () => "TWD",
  };
  const module = new vm.SourceTextModule(await readFile("internal/app/webapp/static/app.js", "utf8"), { context });
  await module.link(() => new vm.SyntheticModule(Object.keys(functions), function () {
    for (const [name, fn] of Object.entries(functions)) this.setExport(name, fn);
  }, { context }));
  await module.evaluate();
  await new Promise(setImmediate);
  return calls;
}

const TELEGRAM = { initData: "user=a&hash=b" };

test("someone the server refuses never initializes the form", async () => {
  const calls = await bootstrap(Promise.resolve("forbidden"), TELEGRAM);
  assert.deepEqual(calls, ["loading", "bootstrap", "forbidden"]);
});

test("outside Telegram nothing is asked of the server", async () => {
  const calls = await bootstrap(Promise.resolve("ok"));
  assert.deepEqual(calls, ["loading", "forbidden"]);
});

test("loading keeps the form and every view unavailable", async () => {
  const calls = await bootstrap(new Promise(() => {}), TELEGRAM);
  assert.deepEqual(calls, ["loading", "bootstrap"]);
});

test("a failed load leaves scoped operations unavailable", async () => {
  const calls = await bootstrap(Promise.resolve("error"), TELEGRAM);
  assert.deepEqual(calls, ["loading", "bootstrap", "trip-error"]);
});

test("an empty trip list is onboarding, not a failure", async () => {
  const calls = await bootstrap(Promise.resolve("empty"), TELEGRAM);
  assert.deepEqual(calls, ["loading", "bootstrap", "trip-empty"]);
});

test("a successful load initializes the form before showing the app", async () => {
  const calls = await bootstrap(Promise.resolve("ok"), TELEGRAM);
  assert.deepEqual(calls, ["loading", "bootstrap", "form", "edit", "numpad", "app"]);
});

async function tripSwitcher(switchResult) {
  const views = [];
  const select = { value: "3", disabled: false, textContent: "", appendChild() {} };
  const input = { value: "" };
  const body = { dataset: {} };
  let requests = 0;
  const context = vm.createContext({
    URLSearchParams,
    document: { body, getElementById: (id) => id === "trip-select" ? select : input, createElement: () => ({}) },
    window: { location: { reload() { views.push("reload"); } } },
    console: { error() {} },
    fetch: async () => ++requests === 1 ? { ok: true, status: 200, json: async () => ({ users: [], members: [], current: 3, trips: [{ id: 3, title: "Tokyo", label: "Tokyo", currency: "TWD" }, { id: 9, title: "Osaka", label: "Osaka", currency: "JPY" }] }) } : await switchResult,
  });
  const module = new vm.SourceTextModule(await readFile("internal/app/webapp/static/trips.js", "utf8"), { context });
  await module.link(() => new vm.SyntheticModule(["apiUrl", "showView", "showToast"], function () {
    this.setExport("apiUrl", (path) => path);
    this.setExport("showView", (view) => views.push(view));
    this.setExport("showToast", () => views.push("toast"));
  }, { context }));
  await module.evaluate();
  assert.equal(await module.namespace.loadBootstrap({}), "ok");
  select.value = "9";
  const pending = select.onchange();
  await new Promise(setImmediate);
  return { select, input, body, views, pending };
}

test("pending trip switch prevents scoped operations", async () => {
  const state = await tripSwitcher(new Promise(() => {}));
  assert.equal(state.select.disabled, true);
  assert.deepEqual(state.views, ["loading"]);
  assert.equal(state.input.value, "3");
});

test("rejected trip switch restores confirmed trip and stays in the app", async () => {
  const state = await tripSwitcher(Promise.resolve({ ok: false, status: 403 }));
  await state.pending;
  assert.equal(state.select.value, "3");
  assert.equal(state.input.value, "3");
  assert.equal(state.body.dataset.tripId, "3");
  assert.equal(state.select.disabled, false);
  assert.deepEqual(state.views, ["loading", "app", "toast"]);
});

async function nativeForm(success = false) {
  const handlers = {};
  let submitted = 0;
  let callback;
  const button = { active: true, visible: false, setText() {}, onClick(fn) { callback = fn; }, show() { this.visible = true; }, hide() { this.visible = false; }, enable() { this.active = true; }, disable() { this.active = false; }, showProgress() {}, hideProgress() {} };
  let saved = 0;
  const name = { value: "Lunch", classList: { toggle() {}, add() {}, remove() {} }, addEventListener() {}, setAttribute() {}, focus() {} };
  const price = { value: "100", classList: name.classList, setAttribute() {} };
  const form = { addEventListener(name, fn) { handlers[name] = fn; }, requestSubmit() { submitted++; } };
  const submit = { disabled: false, querySelector: () => null };
  const body = { dataset: { tripId: "3" } };
  const ctx = { tg: { MainButton: button } };
  const context = vm.createContext({
    document: { body, getElementById(id) {
      if (id === "expense-form") return form;
      if (id === "submit-btn") return submit;
      if (id === "name") return name;
      if (id === "price") return price;
      if (id === "toast-trigger") return { dataset: { success: String(success) } };
      if (["app", "loading", "trip-error", "forbidden"].includes(id)) return { classList: name.classList };
      return null;
    }, querySelectorAll: () => [], dispatchEvent() {} },
    window: { Telegram: { WebApp: ctx.tg } }, console, CustomEvent,
  });
  const cache = new Map();
  async function load(name) {
    if (cache.has(name)) return cache.get(name);
    let module;
    if (["storage", "exchange-rate"].includes(name)) {
      module = new vm.SyntheticModule(["STORAGE_KEYS", "saveDefaults", "updateExchangeRateVisibility", "fetchExchangeRate"], function () {
        this.setExport("STORAGE_KEYS", {});
        for (const key of ["saveDefaults", "updateExchangeRateVisibility", "fetchExchangeRate"]) this.setExport(key, key === "saveDefaults" ? () => { saved++; } : () => {});
      }, { context });
    } else module = new vm.SourceTextModule(await readFile(`internal/app/webapp/static/${name}.js`, "utf8"), { context });
    cache.set(name, module);
    await module.link((specifier) => load(specifier.replace("./", "").replace(".js", "")));
    return module;
  }
  const auth = await load("auth");
  const formModule = await load("form");
  await auth.evaluate();
  await formModule.evaluate();
  formModule.namespace.setupEventListeners(ctx);
  return { button, submit, handlers, name, price, saved: () => saved, showView: auth.namespace.showView, click: () => callback(), submitted: () => submitted };
}

test("native submit is blocked during trip switch and after switch failure", async () => {
  const state = await nativeForm();
  state.showView("app");
  assert.equal(state.button.active, true);
  state.showView("loading");
  state.click();
  assert.equal(state.submitted(), 0);
  assert.equal(state.button.active, false);
  assert.equal(state.button.visible, false);
  state.showView("trip-error");
  state.click();
  assert.equal(state.submitted(), 0);
});

test("late expense completion cannot reenable submission during switch", async () => {
  const state = await nativeForm();
  state.showView("app");
  state.click();
  assert.equal(state.submitted(), 1);
  state.showView("loading");
  state.handlers["htmx:finally:request"]({ detail: { ctx: { response: { status: 500 } } } });
  assert.equal(state.button.active, false);
  assert.equal(state.button.visible, false);
  assert.equal(state.submit.disabled, true);
  let prevented = false;
  state.handlers["htmx:before:request"]({ detail: {}, preventDefault() { prevented = true; } });
  assert.equal(prevented, true);
  state.showView("app");
  state.click();
  assert.equal(state.submitted(), 2);
});


test("htmx 4 requests include encoded Telegram auth and trip scope", async () => {
  const handlers = {};
  await bootstrap(Promise.resolve("forbidden"), { handlers, initData: "user=a&hash=b" });
  const request = { action: "/partial/dashboard?range=all" };
  handlers["htmx:config:request"]({ detail: { ctx: { request } } });
  const url = new URL(request.action, "https://example.test");
  assert.equal(url.searchParams.get("init_data"), "user=a&hash=b");
  assert.equal(url.searchParams.get("trip_id"), "3");
  assert.equal(url.searchParams.get("range"), "all");
});

test("htmx 4 dev requests preserve explicit scope without Telegram auth", async () => {
  const handlers = {};
  await bootstrap(Promise.resolve("forbidden"), { handlers, devMode: true, initData: "secret" });
  const request = { action: "/partial/dashboard?trip_id=9" };
  handlers["htmx:config:request"]({ detail: { ctx: { request } } });
  assert.equal(request.action, "/partial/dashboard?trip_id=9");
});

test("the expense form files a new expense, or changes the one it was opened on", async () => {
  for (const [editing, method] of [[false, "POST"], [true, "PUT"]]) {
    const handlers = {};
    await bootstrap(Promise.resolve("forbidden"), { handlers, initData: "user=a&hash=b", editing });
    const request = { action: "/api/expense", method: "POST", form: { matches: (selector) => selector === "#expense-form" } };
    handlers["htmx:config:request"]({ detail: { ctx: { request } } });
    assert.equal(request.method, method);
  }

  const handlers = {};
  await bootstrap(Promise.resolve("forbidden"), { handlers, initData: "user=a&hash=b", editing: true });
  const request = { action: "/partial/expenses", method: "GET", form: { matches: (selector) => selector === "#list-filters" } };
  handlers["htmx:config:request"]({ detail: { ctx: { request } } });
  assert.equal(request.method, "GET", "only the expense form is changed to a PUT");
});

test("htmx 4 completion resets successful expenses after result swap", async () => {
  const state = await nativeForm(true);
  state.showView("app");
  state.handlers["htmx:before:request"]({ detail: { ctx: {} } });
  assert.equal(state.submit.disabled, true);
  state.handlers["htmx:finally:request"]({ detail: { ctx: { response: { status: 200 } } } });
  assert.equal(state.submit.disabled, false);
  assert.equal(state.saved(), 1);
  assert.equal(state.name.value, "");
  assert.equal(state.price.value, "");
});

for (const response of [undefined, { status: 500 }]) {
  test(`htmx 4 failed completion preserves input (${response?.status ?? "network"})`, async () => {
    const state = await nativeForm(true);
    state.showView("app");
    state.handlers["htmx:before:request"]({ detail: { ctx: {} } });
    state.handlers["htmx:finally:request"]({ detail: { ctx: { response } } });
    assert.equal(state.submit.disabled, false);
    assert.equal(state.saved(), 0);
    assert.equal(state.name.value, "Lunch");
    assert.equal(state.price.value, "100");
  });
}
