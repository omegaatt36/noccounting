import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";

async function bootstrap(authorized, tripsReady) {
  const calls = [];
  const context = vm.createContext({
    document: {
      getElementById: () => null,
      body: { addEventListener() {} },
      addEventListener() {},
    },
  });
  const functions = {
    initTelegram: () => ({}),
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

async function nativeForm() {
  const handlers = {};
  let submitted = 0;
  let callback;
  const button = { active: true, visible: false, setText() {}, onClick(fn) { callback = fn; }, show() { this.visible = true; }, hide() { this.visible = false; }, enable() { this.active = true; }, disable() { this.active = false; }, showProgress() {}, hideProgress() {} };
  const classes = { toggle() {}, add() {}, remove() {} };
  const form = { addEventListener(name, fn) { handlers[name] = fn; }, requestSubmit() { submitted++; } };
  const submit = { disabled: false, querySelector: () => null };
  const body = { dataset: { tripId: "3" } };
  const ctx = { tg: { MainButton: button } };
  const context = vm.createContext({
    document: { body, getElementById(id) {
      if (id === "expense-form") return form;
      if (id === "submit-btn") return submit;
      if (id === "name") return { value: "Lunch", classList: classes, addEventListener() {} };
      if (id === "price") return { value: "100", classList: classes };
      if (["app", "loading", "trip-error", "forbidden"].includes(id)) return { classList: classes };
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
        for (const key of ["saveDefaults", "updateExchangeRateVisibility", "fetchExchangeRate"]) this.setExport(key, () => {});
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
  return { button, submit, handlers, showView: auth.namespace.showView, click: () => callback(), submitted: () => submitted };
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
  state.handlers["htmx:afterRequest"]({ detail: { successful: false } });
  assert.equal(state.button.active, false);
  assert.equal(state.button.visible, false);
  assert.equal(state.submit.disabled, true);
  let prevented = false;
  state.handlers["htmx:beforeRequest"]({ detail: {}, preventDefault() { prevented = true; } });
  assert.equal(prevented, true);
  state.showView("app");
  state.click();
  assert.equal(state.submitted(), 2);
});
