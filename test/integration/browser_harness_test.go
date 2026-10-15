package integration_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/webapp"
	userrepo "github.com/omegaatt36/noccounting/internal/repository/user"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

// The browser tests run the real server (middleware and security headers
// included) in front of in-memory repositories and drive the Mini App through
// headless Chrome, the way a person in Telegram would.

const browserBotToken = "browser-bot-token"

// appOptions picks how the app under test is served.
type appOptions struct {
	// telegram serves the app as Telegram does: init_data is signed and checked,
	// and a Telegram SDK stub stands in for telegram.org's script.
	telegram bool
	// scheme is the Telegram colorScheme, "dark" when empty.
	scheme string
	// userID is who Telegram says is opening the app, 12345 (Alice) when zero.
	userID int64
	// badSignature corrupts the init_data, as a forged one would be.
	badSignature bool
	// noTrips leaves the service account on no trip.
	noTrips bool
	// tripsDown makes the trip listing fail from the start.
	tripsDown bool
	// extraTrips are additional trips available to switch to.
	extraTrips []domain.Trip
	// analyzer is the receipt analyzer for /api/receipt/analyze.
	analyzer expense.ReceiptAnalyzer
}

type browserApp struct {
	t       *testing.T
	baseURL string
	ctx     context.Context

	repo  *inMemoryAccountingRepo
	trips *inMemoryTripRepo

	sdk string
	// failPost makes the browser's POSTs and PUTs to /api/expense fail on the network.
	failPost atomic.Bool
	// failRates makes the browser's GETs to /api/rates fail on the network.
	failRates atomic.Bool

	mu       sync.Mutex
	problems []string
}

func chromePath() string {
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	const mac = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if _, err := os.Stat(mac); err == nil {
		return mac
	}
	return ""
}

func newBrowserApp(t *testing.T, opts appOptions) *browserApp {
	t.Helper()

	chrome := chromePath()
	if chrome == "" {
		// CI has Chrome; a missing one there is a broken runner, not a reason to skip.
		if os.Getenv("CI") != "" {
			t.Fatal("no Chrome found for the browser tests")
		}
		t.Skip("no Chrome found; install it to run the browser tests")
	}

	app := &browserApp{t: t}
	if opts.userID == 0 {
		opts.userID = 12345
	}

	app.trips = &inMemoryTripRepo{}
	if !opts.noTrips {
		app.trips.trips = append([]domain.Trip{{ID: 10, Title: "2026 Tokyo Adventure", Currency: domain.CurrencyTWD}}, opts.extraTrips...)
	}
	if opts.tripsDown {
		app.trips.setError(fmt.Errorf("TREK is down"))
	}
	app.repo = newInMemoryAccountingRepo()

	userSvc := user.NewService(userrepo.NewRepo("12345:8:Alice,67890:9:Bob"))
	expSvc := expense.NewService(app.repo, staticRateFetcher{}, opts.analyzer)
	tripSvc := trip.NewService(app.trips)

	server, err := webapp.NewServer(userSvc, expSvc, tripSvc, "0", browserBotToken, !opts.telegram)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	app.baseURL = httpServer.URL
	t.Cleanup(func() {
		httpServer.Close()
		_ = server.Shutdown(context.Background())
	})

	if opts.telegram {
		scheme := opts.scheme
		if scheme == "" {
			scheme = "dark"
		}
		initData := signedInitData(browserBotToken, opts.userID)
		if opts.badSignature {
			initData = strings.Replace(initData, "hash=", "hash=0", 1)
		}
		app.sdk = telegramSDK(initData, opts.userID, scheme)
	}

	allocOpts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocOpts = append(allocOpts, chromedp.ExecPath(chrome))
	if os.Getenv("CI") != "" {
		allocOpts = append(allocOpts, chromedp.NoSandbox)
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	ctx, cancelTimeout := context.WithTimeout(browserCtx, 90*time.Second)
	t.Cleanup(func() {
		cancelTimeout()
		cancelBrowser()
		cancelAlloc()
	})
	app.ctx = ctx

	watchBrowserEvent(app, runtime.ExceptionThrown)
	watchBrowserEvent(app, runtime.ConsoleAPICalled)
	watchBrowserEvent(app, log.EntryAdded)
	watchBrowserEvent(app, page.JavascriptDialogOpening)
	watchBrowserEvent(app, fetch.RequestPaused)
	app.run(
		chromedp.EmulateViewport(390, 844, chromedp.EmulateMobile),
		chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
			if _, err := cdp.Call(ctx, target, page.Enable, page.EnableParams{}); err != nil {
				return err
			}
			if _, err := cdp.Call(ctx, target, log.Enable, cdp.Empty{}); err != nil {
				return err
			}
			_, err := cdp.Call(ctx, target, fetch.Enable, fetch.EnableParams{Patterns: []*fetch.RequestPattern{
				{URLPattern: "https://telegram.org/*"},
				{URLPattern: "*/api/expense*"},
				{URLPattern: "*/api/rates*"},
			}})
			return err
		}),
	)
	return app
}

// signedInitData is init_data as Telegram signs it for the bot token.
func signedInitData(token string, userID int64) string {
	values := url.Values{
		"auth_date": {strconv.FormatInt(time.Now().Unix(), 10)},
		"query_id":  {"AAHbrowsertest"},
		"user":      {fmt.Sprintf(`{"id":%d,"first_name":"Tester"}`, userID)},
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+values.Get(key))
	}

	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	signature := hmac.New(sha256.New, secret.Sum(nil))
	signature.Write([]byte(strings.Join(lines, "\n")))
	values.Set("hash", hex.EncodeToString(signature.Sum(nil)))
	return values.Encode()
}

// telegramSDK is a stand-in for telegram.org/js/telegram-web-app.js that
// records what the app asks of Telegram on window.__tg, where the tests read it.
func telegramSDK(initData string, userID int64, scheme string) string {
	quote := func(s string) string {
		encoded, _ := json.Marshal(s)
		return string(encoded)
	}
	return fmt.Sprintf(`(() => {
  const handlers = {};
  const button = {
    text: "", color: "", textColor: "", isVisible: false, isActive: true, isProgressVisible: false, click: null,
    setText(text) { this.text = text; return this; },
    onClick(callback) { this.click = callback; return this; },
    offClick() { return this; },
    show() { this.isVisible = true; return this; },
    hide() { this.isVisible = false; return this; },
    enable() { this.isActive = true; return this; },
    disable() { this.isActive = false; return this; },
    showProgress() { this.isProgressVisible = true; return this; },
    hideProgress() { this.isProgressVisible = false; return this; },
    setParams(params) {
      if (params.color) this.color = params.color;
      if (params.text_color) this.textColor = params.text_color;
      return this;
    },
  };
  const back = {
    isVisible: false,
    show() { this.isVisible = true; return this; },
    hide() { this.isVisible = false; return this; },
    onClick() { return this; },
    offClick() { return this; },
  };
  window.__tg = {
    header: "", background: "", haptics: [], confirms: [], mainButton: button,
    fire(name) { handlers[name]?.(); },
  };
  window.Telegram = { WebApp: {
    initData: %s,
    initDataUnsafe: { user: { id: %d, first_name: "Tester" } },
    colorScheme: %s,
    ready() {}, expand() {}, disableVerticalSwipes() {}, enableClosingConfirmation() {},
    setHeaderColor(color) { window.__tg.header = color; },
    setBackgroundColor(color) { window.__tg.background = color; },
    onEvent(name, callback) { handlers[name] = callback; },
    showConfirm(msg, callback) { window.__tg.confirms.push(msg); callback?.(true); },
    HapticFeedback: {
      impactOccurred() {}, selectionChanged() {},
      notificationOccurred(style) { window.__tg.haptics.push(style); },
    },
    MainButton: button, BackButton: back,
  } };
})();`, quote(initData), userID, quote(scheme))
}

// watchBrowserEvent subscribes before returning, so setup and navigation cannot
// race with the goroutine that consumes the events. Canceling the browser context
// ends each subscription and its reader.
func watchBrowserEvent[E any](a *browserApp, event cdp.Event[E]) {
	events := chromedp.Events(a.ctx, event)
	go func() {
		for payload, err := range events {
			if err != nil {
				if a.ctx.Err() == nil {
					a.addProblem(fmt.Sprintf("receiving %s: %v", event.Method, err))
				}
				return
			}
			a.onEvent(&payload)
		}
	}()
}

func (a *browserApp) onEvent(ev any) {
	switch e := ev.(type) {
	case *runtime.EventExceptionThrown:
		a.addProblem("exception: " + (&chromedp.ExceptionError{ExceptionDetails: e.ExceptionDetails}).Error())
	case *runtime.EventConsoleAPICalled:
		if e.Type == runtime.ConsoleAPICalledTypeError {
			parts := make([]string, 0, len(e.Args))
			for _, arg := range e.Args {
				if len(arg.Value) > 0 {
					parts = append(parts, string(arg.Value))
				} else {
					parts = append(parts, arg.Description)
				}
			}
			a.addProblem("console.error: " + strings.Join(parts, " "))
		}
	case *log.EventEntryAdded:
		if e.Entry.Level == log.EntryLevelError {
			a.addProblem(fmt.Sprintf("%s: %s %s", e.Entry.Source, e.Entry.Text, e.Entry.URL))
		}
	case *page.EventJavascriptDialogOpening:
		// hx-confirm asks before a delete; the person says yes.
		go func() {
			_, _ = cdp.Call(a.ctx, chromedp.FromContext(a.ctx).Target, page.HandleJavaScriptDialog, page.HandleJavaScriptDialogParams{Accept: true})
		}()
	case *fetch.EventRequestPaused:
		go a.route(e)
	}
}

func (a *browserApp) route(e *fetch.EventRequestPaused) {
	target := chromedp.FromContext(a.ctx).Target

	var err error
	switch {
	case strings.HasPrefix(e.Request.URL, "https://telegram.org/"):
		_, err = cdp.Call(a.ctx, target, fetch.FulfillRequest, fetch.FulfillRequestParams{
			RequestID:       e.RequestID,
			ResponseCode:    200,
			ResponseHeaders: []*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/javascript"}},
			Body:            []byte(a.sdk),
		})
	case (e.Request.Method == "POST" || e.Request.Method == "PUT") && a.failPost.Load():
		_, err = cdp.Call(a.ctx, target, fetch.FailRequest, fetch.FailRequestParams{
			RequestID: e.RequestID, ErrorReason: network.ErrorReasonConnectionFailed,
		})
	case strings.Contains(e.Request.URL, "/api/rates") && a.failRates.Load():
		_, err = cdp.Call(a.ctx, target, fetch.FailRequest, fetch.FailRequestParams{
			RequestID: e.RequestID, ErrorReason: network.ErrorReasonConnectionFailed,
		})
	default:
		_, err = cdp.Call(a.ctx, target, fetch.ContinueRequest, fetch.ContinueRequestParams{RequestID: e.RequestID})
	}
	if err != nil && a.ctx.Err() == nil {
		a.addProblem(fmt.Sprintf("routing %s: %v", e.Request.URL, err))
	}
}

func (a *browserApp) addProblem(problem string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.problems = append(a.problems, problem)
}

// requireNoProblems fails the test on anything the page reported to its
// console: script errors, failed requests, and CSP violations alike.
func (a *browserApp) requireNoProblems() {
	a.t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.problems) > 0 {
		a.t.Fatalf("the page reported problems:\n  %s", strings.Join(a.problems, "\n  "))
	}
}

func (a *browserApp) run(actions ...chromedp.Action[chromedp.Void]) {
	a.t.Helper()
	if err := chromedp.Do(a.ctx, actions...); err != nil {
		a.t.Fatal(err)
	}
}

func (a *browserApp) open() {
	a.t.Helper()
	a.run(chromedp.Navigate(a.baseURL + "/"))
}

// evalBrowser awaits a JavaScript expression and decodes its result into T.
func evalBrowser[T any](a *browserApp, expression string) T {
	a.t.Helper()
	out, err := chromedp.Run(a.ctx, chromedp.Evaluate[T](expression, chromedp.EvalAwaitPromise))
	if err != nil {
		a.t.Fatal(err)
	}
	return out
}

func (a *browserApp) text(expression string) string {
	a.t.Helper()
	return evalBrowser[string](a, expression)
}

func (a *browserApp) flag(expression string) bool {
	a.t.Helper()
	return evalBrowser[bool](a, expression)
}

// waitFor blocks until the JS expression is truthy. A navigation that replaces
// the page mid-poll (a reload) only restarts the wait.
func (a *browserApp) waitFor(what, expression string) {
	a.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		truthy, err := chromedp.Run(a.ctx, chromedp.Evaluate[bool](`!!(`+expression+`)`))
		if err == nil && truthy {
			return
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.t.Fatalf("waiting for %s: gave up after 15s (last error: %v)\npage problems: %v", what, lastErr, a.problems)
}

func (a *browserApp) click(selector string) {
	a.t.Helper()
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	if err := chromedp.Do(ctx,
		chromedp.Evaluate[chromedp.Void](fmt.Sprintf(`document.querySelector(%q)?.scrollIntoView({block: "center", behavior: "instant"})`, selector)),
		chromedp.Click(chromedp.CSS(selector), chromedp.NodeVisible),
	); err != nil {
		a.t.Fatalf("clicking %s: %v", selector, err)
	}
}

func (a *browserApp) waitReady() {
	a.t.Helper()
	a.waitFor("the app to be ready", `document.body.dataset.appReady === "true"`)
}

func (a *browserApp) visible(selector string) string {
	return fmt.Sprintf(`(() => { const el = document.querySelector(%q); return !!el && el.getClientRects().length > 0; })()`, selector)
}

func (a *browserApp) expenseCount() int {
	a.repo.mu.Lock()
	defer a.repo.mu.Unlock()
	return len(a.repo.expenses)
}

// enterPrice types an amount on the numpad the way a thumb would.
func (a *browserApp) enterPrice(amount string) {
	a.t.Helper()
	a.click("#price")
	a.waitFor("the numpad", a.visible("#numpad-sheet"))
	for _, digit := range amount {
		a.click(fmt.Sprintf(`[data-numpad-key="%c"]`, digit))
	}
	a.click("#numpad-confirm")
	a.waitFor("the numpad to close", `document.getElementById("numpad-sheet").classList.contains("hidden")`)
}

// fillExpense fills the form in, leaving it ready to submit.
func (a *browserApp) fillExpense(name, amount string) {
	a.t.Helper()
	a.run(chromedp.SendKeys(chromedp.CSS("#name"), name))
	a.enterPrice(amount)
}
