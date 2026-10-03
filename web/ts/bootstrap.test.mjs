import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";

async function bootstrap(authorized, tripsReady, options = {}) {
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
    authenticate: async () => authorized,
    showView: (view) => calls.push(view),
    loadTrips: async () => { calls.push("trips"); return await tripsReady; },
    loadMembers: async () => calls.push("members"),
    setupEventListeners: () => calls.push("form"),
    setupNumpad: () => calls.push("numpad"),
    restoreDefaults() {}, updateExchangeRateVisibility() {}, fetchExchangeRate() {},
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

test("failed authentication never initializes trips or form", async () => {
  const calls = await bootstrap(false, Promise.resolve(true));
  assert.deepEqual(calls, []);
});

test("trip loading keeps the form and dashboard unavailable", async () => {
  const calls = await bootstrap(true, new Promise(() => {}));
  assert.deepEqual(calls, ["trips"]);
});

test("trip load failure leaves scoped operations unavailable", async () => {
  const calls = await bootstrap(true, Promise.resolve(false));
  assert.deepEqual(calls, ["trips", "trip-error"]);
});

test("successful trip load initializes form before showing app", async () => {
  const calls = await bootstrap(true, Promise.resolve(true));
  assert.deepEqual(calls, ["trips", "members", "form", "numpad", "app"]);
});

async function tripSwitcher(switchResult) {
  const views = [];
  const listeners = {};
  const select = { value: "3", disabled: false, textContent: "", appendChild() {}, addEventListener(name, fn) { listeners[name] = fn; } };
  const input = { value: "" };
  const body = { dataset: {} };
  let requests = 0;
  const context = vm.createContext({
    URLSearchParams,
    document: { body, getElementById: (id) => id === "trip-select" ? select : input, createElement: () => ({}) },
    window: { location: { reload() { views.push("reload"); } } },
    console: { error() {} },
    fetch: async () => ++requests === 1 ? { ok: true, json: async () => ({ current: 3, trips: [{ id: 3, title: "Tokyo", label: "Tokyo", currency: "TWD" }, { id: 9, title: "Osaka", label: "Osaka", currency: "JPY" }] }) } : await switchResult,
  });
  const module = new vm.SourceTextModule(await readFile("internal/app/webapp/static/trips.js", "utf8"), { context });
  await module.link(() => new vm.SyntheticModule(["apiUrl", "showView"], function () {
    this.setExport("apiUrl", (path) => path);
    this.setExport("showView", (view) => views.push(view));
  }, { context }));
  await module.evaluate();
  await module.namespace.loadTrips({});
  select.value = "9";
  const pending = listeners.change();
  await new Promise(setImmediate);
  return { select, input, body, views, pending };
}

test("pending trip switch prevents scoped operations", async () => {
  const state = await tripSwitcher(new Promise(() => {}));
  assert.equal(state.select.disabled, true);
  assert.deepEqual(state.views, ["loading"]);
  assert.equal(state.input.value, "3");
});

test("rejected trip switch restores confirmed trip and keeps app unavailable", async () => {
  const state = await tripSwitcher(Promise.resolve({ ok: false, status: 403 }));
  await state.pending;
  assert.equal(state.select.value, "3");
  assert.equal(state.input.value, "3");
  assert.equal(state.body.dataset.tripId, "3");
  assert.deepEqual(state.views, ["loading", "trip-error"]);
});

async function nativeForm(success = false) {
  const handlers = {};
  let submitted = 0;
  let callback;
  const button = { active: true, visible: false, setText() {}, onClick(fn) { callback = fn; }, show() { this.visible = true; }, hide() { this.visible = false; }, enable() { this.active = true; }, disable() { this.active = false; }, showProgress() {}, hideProgress() {} };
  let saved = 0;
  const name = { value: "Lunch", classList: { toggle() {}, add() {}, remove() {} }, addEventListener() {}, focus() {} };
  const price = { value: "100", classList: name.classList };
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
    }, querySelectorAll: () => [] },
    window: { Telegram: { WebApp: ctx.tg } }, console,
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
  await bootstrap(false, Promise.resolve(false), { handlers, initData: "user=a&hash=b" });
  const request = { action: "/partial/dashboard?range=all" };
  handlers["htmx:config:request"]({ detail: { ctx: { request } } });
  const url = new URL(request.action, "https://example.test");
  assert.equal(url.searchParams.get("init_data"), "user=a&hash=b");
  assert.equal(url.searchParams.get("trip_id"), "3");
  assert.equal(url.searchParams.get("range"), "all");
});

test("htmx 4 dev requests preserve explicit scope without Telegram auth", async () => {
  const handlers = {};
  await bootstrap(false, Promise.resolve(false), { handlers, devMode: true, initData: "secret" });
  const request = { action: "/partial/dashboard?trip_id=9" };
  handlers["htmx:config:request"]({ detail: { ctx: { request } } });
  assert.equal(request.action, "/partial/dashboard?trip_id=9");
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
