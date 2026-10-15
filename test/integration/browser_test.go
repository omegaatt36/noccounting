package integration_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omegaatt36/noccounting/domain"
)

func TestBrowserLogAndReviewAnExpense(t *testing.T) {
	app := newBrowserApp(t, appOptions{})
	app.open()
	app.waitReady()

	// The statistics are opened while nothing is spent, then an expense is filed:
	// opening them again must show that expense, not the draw from before.
	app.click("#nav-stats")
	app.waitFor("the empty statistics", `document.getElementById("dashboard-content")?.innerText.includes("0 筆")`)
	app.waitFor("the empty-state call to action", app.visible(`#view-stats [data-action="show-form"]`))
	app.click(`#view-stats [data-action="show-form"]`)
	app.waitFor("the form", app.visible("#expense-form"))

	app.fillExpense("東京拉麵", "1200")
	assert.Equal(t, "1200", app.text(`document.getElementById("price").value`))
	app.click("#submit-btn")
	app.waitFor("the success toast", `document.querySelector("#result [data-tui-toast]")?.textContent.includes("東京拉麵")`)

	require.Equal(t, 1, app.expenseCount())
	filed := app.expenseAt(0)
	assert.Equal(t, "東京拉麵", filed.Name)
	assert.Equal(t, uint64(1200), filed.Price)
	assert.Equal(t, domain.CurrencyTWD, filed.Currency)
	assert.Equal(t, "8", filed.PaidByID)
	assert.Equal(t, "", app.text(`document.getElementById("name").value`), "the form is cleared for the next expense")

	app.click("#nav-stats")
	app.waitFor("the new expense in the statistics", `document.getElementById("dashboard-content")?.innerText.includes("1 筆")`)

	// A statistics row opens the list narrowed to what it sums up.
	app.click(`button[data-list-category="food"]`)
	app.waitFor("the list", app.visible("#view-list"))
	app.waitFor("the expense in the list", `document.getElementById("list-content")?.innerText.includes("東京拉麵")`)
	assert.Equal(t, "food", app.text(`document.getElementById("list-category").value`), "the list is narrowed to the category")

	// Searching narrows it further.
	app.run(chromedp.SendKeys("#list-q", "沒有這個", chromedp.ByQuery))
	app.waitFor("the empty search", `document.getElementById("list-content")?.innerText.includes("沒有符合條件的消費")`)
	app.run(chromedp.Evaluate(`(() => {
		const q = document.getElementById("list-q");
		q.value = "";
		q.dispatchEvent(new Event("input", { bubbles: true }));
	})()`, nil))
	app.waitFor("the list again", `document.getElementById("list-content")?.innerText.includes("東京拉麵")`)

	// The settlement is a tab of its own.
	app.click("#nav-settle")
	app.waitFor("the settlement", `document.getElementById("settlement-content")?.innerText.includes("各人餘額")`)

	// A row opens the form on that expense; saving changes it in place.
	app.click("#nav-list")
	app.waitFor("the list", app.visible(`#list-content [data-edit]`))
	app.click(`#list-content [data-edit]`)
	app.waitFor("the editor", app.visible("#expense-form")+` && document.getElementById("form-title").textContent === "編輯消費"`)
	assert.Equal(t, "東京拉麵", app.text(`document.getElementById("name").value`))
	assert.Equal(t, "1200", app.text(`document.getElementById("price").value`))
	assert.Equal(t, "儲存變更", strings.TrimSpace(app.text(`document.getElementById("submit-label").textContent`)))
	assert.False(t, app.flag(app.visible("#receipt-row")), "scanning a receipt is for new expenses")

	app.run(chromedp.SetValue("#name", "東京沾麵", chromedp.ByQuery))
	app.click("#price")
	app.waitFor("the numpad", app.visible("#numpad-sheet"))
	app.click("#numpad-clear")
	for _, digit := range "1500" {
		app.click(fmt.Sprintf(`[data-numpad-key="%c"]`, digit))
	}
	app.click("#numpad-confirm")
	app.click("#submit-btn")
	app.waitFor("the update toast", toastSaying("已更新"))
	app.waitFor("the list", app.visible("#view-list"))

	require.Equal(t, 1, app.expenseCount(), "editing changes an expense and does not file another")
	edited := app.expenseAt(0)
	assert.Equal(t, "東京沾麵", edited.Name)
	assert.Equal(t, uint64(1500), edited.Price)
	assert.Equal(t, "8", edited.PaidByID, "what the form left alone is kept")
	app.waitFor("the edit in the list", `document.getElementById("list-content")?.innerText.includes("東京沾麵")`)
	assert.Equal(t, "新增消費", strings.TrimSpace(app.text(`document.getElementById("form-title").textContent`)), "the form is for new expenses again")
	assert.Equal(t, "", app.text(`document.getElementById("expense-id").value`))

	// Leaving the editor without saving changes nothing.
	app.click(`#list-content [data-edit]`)
	app.waitFor("the editor", `document.getElementById("form-title").textContent === "編輯消費"`)
	app.click(`[data-action="cancel-edit"]`)
	app.waitFor("the list", app.visible("#view-list"))
	assert.Equal(t, "", app.text(`document.getElementById("name").value`), "the half-edited form is emptied")

	// Deleting happens from the editor, and the list redraws without it.
	app.click(`#list-content [data-edit]`)
	app.waitFor("the editor", `document.getElementById("form-title").textContent === "編輯消費"`)
	app.click(`[data-action="delete-expense"]`)
	// The list keeps the category filter it was narrowed to, so it reads as emptied.
	app.waitFor("the list to redraw empty", `document.getElementById("list-content")?.innerText.includes("沒有符合條件的消費")`)
	assert.Equal(t, 0, app.expenseCount())

	// A page that leaves its console clean ran under the CSP without a single
	// blocked script, style, or request.
	app.requireNoProblems()
}

func TestBrowserToastsKeepEditorActionsClickable(t *testing.T) {
	app := newBrowserApp(t, appOptions{})
	app.open()
	app.waitReady()
	// Keep notifications on screen so this checks overlap, not timer speed.
	app.run(chromedp.Evaluate(`(() => {
		const schedule = window.setTimeout;
		window.setTimeout = (fn, delay, ...args) => schedule(fn, delay >= 3000 ? 60000 : delay, ...args);
	})()`, nil))
	app.fillExpense("通知遮擋測試", "100")
	app.click("#submit-btn")
	app.waitFor("the saved toast", toastSaying("通知遮擋測試"))
	app.click("#nav-list")
	app.waitFor("the expense", app.visible(`#list-content [data-edit]`))
	app.click(`#list-content [data-edit]`)
	app.waitFor("the editor", app.visible("#edit-bar"))

	assertActionsReachable := func() {
		t.Helper()
		app.run(chromedp.Evaluate(`window.scrollTo(0, 0)`, nil))
		// Check inside the upper half: font metrics change whether the toast
		// also covers the center of these buttons on a particular platform.
		for _, action := range []string{"cancel-edit", "delete-expense"} {
			require.True(t, app.flag(fmt.Sprintf(`(() => {
				const button = document.querySelector('[data-action="%s"]');
				const r = button.getBoundingClientRect();
				return button.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 4));
			})()`, action)), "%s must receive clicks while a toast is visible", action)
		}
	}
	assertActionsReachable()
	// The server toast still has a working close button.
	app.click("[data-tui-toast-dismiss]")
	app.waitFor("the toast to close", `!document.querySelector("#result [data-tui-toast]")`)
	// Client-side notifications use the same click-through behavior.
	app.run(chromedp.Evaluate(`import("/static/toast.js").then(m => m.showToast("已刪除", "通知遮擋測試", "success"))`, nil))
	app.waitFor("the client toast", toastSaying("已刪除"))
	assertActionsReachable()
	app.click(`[data-action="cancel-edit"]`)
	app.waitFor("the list", app.visible("#view-list"))
	app.requireNoProblems()
}

func TestBrowserEditFailureKeepsTheEditor(t *testing.T) {
	app := newBrowserApp(t, appOptions{})
	app.repo.expenses = []domain.Expense{{
		ID: "exp-0", Name: "電車", Price: 480, Currency: domain.CurrencyJPY, ExchangeRate: decimal.RequireFromString("0.22"),
		Category: domain.CategoryTransport, Method: domain.PaymentMethodIcCard, PaidByID: "9", ParticipantIDs: []string{"8"},
		ShoppedAt: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
	}}
	app.open()
	app.waitReady()

	app.click("#nav-list")
	app.waitFor("the list", app.visible(`#list-content [data-edit]`))
	app.click(`#list-content [data-edit]`)
	app.waitFor("the editor", `document.getElementById("form-title").textContent === "編輯消費"`)

	// The editor shows the expense as it was filed, not as the defaults would.
	assert.Equal(t, "JPY", app.text(`document.getElementById("currency-input").value`))
	assert.Equal(t, "0.22", app.text(`document.getElementById("exchange-rate-input").value`), "the stored rate is not replaced by today's")
	assert.Equal(t, "transport", app.text(`document.getElementById("category-input").value`))
	assert.Equal(t, "ic_card", app.text(`document.getElementById("method-input").value`))
	assert.Equal(t, "2026-10-03", app.text(`document.getElementById("date-input").value`))
	assert.Equal(t, "67890", app.text(`document.getElementById("paid-by-select").value`), "the payer, by the id the picker knows them by")
	assert.Equal(t, "8", app.text(`[...document.querySelectorAll('#participants input:checked')].map((box) => box.value).join(",")`))

	app.failPost.Store(true)
	app.click("#submit-btn")
	app.waitFor("the failure toast", toastSaying("儲存失敗"))
	assert.Equal(t, "編輯消費", app.text(`document.getElementById("form-title").textContent`), "the editor stays open to try again")
	assert.Equal(t, "電車", app.text(`document.getElementById("name").value`))

	app.failPost.Store(false)
	app.click("#submit-btn")
	app.waitFor("the list", app.visible("#view-list"))
	app.waitFor("the form to be for new expenses again", `document.getElementById("form-title").textContent === "新增消費"`)
	assert.Equal(t, uint64(480), app.expenseAt(0).Price)
	assert.Equal(t, "9", app.expenseAt(0).PaidByID)
}

// toastSaying is true while any toast on screen has the text; an earlier toast
// may still be showing in front of the one being waited for.
func toastSaying(text string) string {
	return fmt.Sprintf(`[...document.querySelectorAll("#result [data-tui-toast]")].some((el) => el.textContent.includes(%q))`, text)
}

func (a *browserApp) expenseAt(i int) domain.Expense {
	a.repo.mu.Lock()
	defer a.repo.mu.Unlock()
	return a.repo.expenses[i]
}

func TestBrowserEnforcesContentSecurityPolicy(t *testing.T) {
	app := newBrowserApp(t, appOptions{})
	app.open()
	app.waitReady()

	csp := app.text(`fetch("/").then((response) => response.headers.get("content-security-policy"))`)
	assert.Contains(t, csp, "script-src 'self' https://telegram.org")

	injected := false
	app.eval(`(() => {
		const script = document.createElement("script");
		script.textContent = "window.__injected = true";
		document.head.appendChild(script);
		return window.__injected === true;
	})()`, &injected)
	assert.False(t, injected, "an inline script must be refused")
}

func TestBrowserSubmitFailureKeepsTheForm(t *testing.T) {
	app := newBrowserApp(t, appOptions{})
	app.open()
	app.waitReady()

	app.fillExpense("電車", "480")
	app.failPost.Store(true)
	app.click("#submit-btn")
	app.waitFor("the failure toast", `document.querySelector("#result [data-tui-toast]")?.textContent.includes("網路連線有問題")`)
	assert.Equal(t, "電車", app.text(`document.getElementById("name").value`), "what was typed survives the failure")
	assert.Equal(t, "480", app.text(`document.getElementById("price").value`))
	assert.Equal(t, 0, app.expenseCount())
	assert.True(t, app.submitEnabled(), "the person can try again")

	app.failPost.Store(false)
	app.click("#submit-btn")
	app.waitFor("the success toast", `document.querySelector("#result [data-tui-toast][data-variant='success']") !== null`)
	assert.Equal(t, 1, app.expenseCount(), "one retry files one expense")
}

func (a *browserApp) submitEnabled() bool {
	var enabled bool
	a.waitFor("the submit button", `document.getElementById("submit-btn").disabled === false`)
	a.eval(`document.getElementById("submit-btn").disabled === false`, &enabled)
	return enabled
}

func TestBrowserTripStates(t *testing.T) {
	t.Run("a service account on no trip is onboarding", func(t *testing.T) {
		app := newBrowserApp(t, appOptions{noTrips: true})
		app.open()
		app.waitFor("the empty-trip page", app.visible("#trip-empty"))
		assert.False(t, app.flag(app.visible("#app")))
	})

	t.Run("a TREK outage offers a retry that recovers", func(t *testing.T) {
		app := newBrowserApp(t, appOptions{tripsDown: true})
		app.open()
		app.waitFor("the trip error", app.visible("#trip-error"))

		app.trips.setError(nil)
		app.click(`#trip-error [data-action="reload"]`)
		app.waitReady()
		assert.True(t, app.flag(app.visible("#app")))
	})
}

func TestBrowserStatisticsFailureOffersRetry(t *testing.T) {
	app := newBrowserApp(t, appOptions{})
	app.open()
	app.waitReady()

	app.repo.mu.Lock()
	app.repo.queryErr = errors.New("TREK is down")
	app.repo.mu.Unlock()

	app.click("#nav-stats")
	app.waitFor("the statistics error", app.visible("#stats-error"))

	app.repo.mu.Lock()
	app.repo.queryErr = nil
	app.repo.mu.Unlock()

	app.click(`[data-action="retry-view"][data-view="stats"]`)
	app.waitFor("the statistics", `document.getElementById("dashboard-content")?.innerText.includes("0 筆")`)
	assert.False(t, app.flag(app.visible("#stats-error")), "recovered statistics drop the error")
}

func TestBrowserTelegramMiniApp(t *testing.T) {
	t.Run("a signed-in member gets the Telegram theme and a MainButton that files the form", func(t *testing.T) {
		app := newBrowserApp(t, appOptions{telegram: true, scheme: "light"})
		app.open()
		app.waitReady()

		assert.Equal(t, "light", app.text(`document.documentElement.dataset.theme`))
		assert.Equal(t, "rgb(255, 252, 240)", app.text(`getComputedStyle(document.body).backgroundColor`))
		assert.Equal(t, "#fffcf0", strings.ToLower(app.text(`window.__tg.header`)), "Telegram's header takes the page color")
		assert.Equal(t, "#205ea6", strings.ToLower(app.text(`window.__tg.mainButton.color`)))
		assert.True(t, app.flag(`window.__tg.mainButton.isVisible`), "the MainButton is on the form")
		assert.False(t, app.flag(app.visible("#submit-btn")), "the in-page button gives way to it")

		app.click("#nav-stats")
		assert.False(t, app.flag(`window.__tg.mainButton.isVisible`), "the statistics have no form to file")
		app.click("#nav-form")
		assert.True(t, app.flag(`window.__tg.mainButton.isVisible`))

		app.run(chromedp.EvaluateAsDevTools(`window.Telegram.WebApp.colorScheme = "dark"; window.__tg.fire("themeChanged")`, nil))
		assert.Equal(t, "dark", app.text(`document.documentElement.dataset.theme`), "the page follows Telegram's theme change")
		assert.Equal(t, "#100f0f", strings.ToLower(app.text(`window.__tg.header`)))
		assert.Equal(t, "#4385be", strings.ToLower(app.text(`window.__tg.mainButton.color`)))

		app.fillExpense("お土産", "3000")
		app.run(chromedp.EvaluateAsDevTools(`window.__tg.mainButton.click()`, nil))
		app.waitFor("the success toast", `document.querySelector("#result [data-tui-toast]")?.textContent.includes("お土産")`)
		require.Equal(t, 1, app.expenseCount())
		assert.Contains(t, app.text(`window.__tg.haptics.join(",")`), "success")

		// Deleting in Telegram mode asks through Telegram.WebApp.showConfirm.
		app.click("#nav-list")
		app.waitFor("the list", app.visible(`#list-content [data-edit]`))
		app.click(`#list-content [data-edit]`)
		app.waitFor("the editor", `document.getElementById("form-title").textContent === "編輯消費"`)
		app.click(`[data-action="delete-expense"]`)
		app.waitFor("the list after delete", app.visible("#view-list"))
		assert.Equal(t, 0, app.expenseCount())
		assert.Contains(t, app.text(`window.__tg.confirms.join(",")`), "確定要刪除這筆消費嗎？")

		app.requireNoProblems()
	})

	t.Run("a forged init_data is refused", func(t *testing.T) {
		app := newBrowserApp(t, appOptions{telegram: true, badSignature: true})
		app.open()
		app.waitFor("the refusal", app.visible("#forbidden"))
		assert.False(t, app.flag(app.visible("#app")))
	})

	t.Run("someone who is not a member is refused", func(t *testing.T) {
		app := newBrowserApp(t, appOptions{telegram: true, userID: 99999})
		app.open()
		app.waitFor("the refusal", app.visible("#forbidden"))
		assert.False(t, app.flag(app.visible("#app")))
	})
}

func TestBrowserAccessibility(t *testing.T) {
	app := newBrowserApp(t, appOptions{})
	app.open()
	app.waitReady()

	assert.NotContains(t, app.text(`document.querySelector('meta[name="viewport"]').content`), "user-scalable=no", "people can pinch-zoom")

	t.Run("the amount sheet is a dialog the keyboard can open and leave", func(t *testing.T) {
		app.run(chromedp.Focus("#price", chromedp.ByQuery), chromedp.KeyEvent(kb.Enter))
		app.waitFor("the numpad", app.visible("#numpad-sheet"))
		assert.Equal(t, "dialog", app.text(`document.getElementById("numpad-sheet").getAttribute("role")`))
		assert.Equal(t, "true", app.text(`document.getElementById("numpad-sheet").getAttribute("aria-modal")`))
		app.waitFor("focus to move into the sheet", `document.activeElement.id === "numpad-confirm"`)

		app.run(chromedp.KeyEvent(kb.Escape))
		app.waitFor("the numpad to close", `document.getElementById("numpad-sheet").classList.contains("hidden")`)
		assert.Equal(t, "price", app.text(`document.activeElement.id`), "focus returns to the field")
	})

	t.Run("every control on screen has a name", func(t *testing.T) {
		app.click("#price") // open the sheet so its keys are counted too
		app.waitFor("the numpad", app.visible("#numpad-sheet"))

		var unnamed []string
		app.eval(`[...document.querySelectorAll("button, select, input:not([type=hidden])")]
			.filter((el) => el.getClientRects().length > 0)
			.filter((el) => !(el.getAttribute("aria-label") || el.labels?.[0]?.textContent.trim() || el.textContent.trim() || el.title))
			.map((el) => el.outerHTML.slice(0, 80))`, &unnamed)
		assert.Empty(t, unnamed)

		app.click("#numpad-confirm")
	})

	t.Run("the pickers are single-choice groups that track the selection", func(t *testing.T) {
		for _, group := range []string{"category-tabs", "currency-tabs", "method-tabs"} {
			checked := app.text(`document.querySelectorAll("#` + group + ` [aria-checked=true]").length + ""`)
			assert.Equal(t, "1", checked, group)
		}

		other := string(domain.CategoryValues()[1])
		app.click(`#category-tabs [data-tui-tabs-value="` + other + `"]`)
		app.waitFor("the choice to be announced", `document.querySelector('#category-tabs [data-tui-tabs-value="`+other+`"]').getAttribute("aria-checked") === "true"`)
		assert.Equal(t, "1", app.text(`document.querySelectorAll("#category-tabs [aria-checked=true]").length + ""`))
	})

	t.Run("the navigation says where you are", func(t *testing.T) {
		assert.Equal(t, "page", app.text(`document.getElementById("nav-form").getAttribute("aria-current")`))
		for _, tab := range []string{"list", "stats", "settle", "form"} {
			app.click("#nav-" + tab)
			assert.Equal(t, "page", app.text(`document.getElementById("nav-`+tab+`").getAttribute("aria-current")`), tab)
			assert.Equal(t, "1", app.text(`document.querySelectorAll("#bottom-nav [aria-current]").length + ""`), "only the tab showing is current")
		}
	})

	app.requireNoProblems()
}

type browserReceiptAnalyzer struct {
	analysis *domain.ReceiptAnalysis
	err      error
}

func (b browserReceiptAnalyzer) Analyze(_ context.Context, _ []byte) (*domain.ReceiptAnalysis, error) {
	return b.analysis, b.err
}

func TestBrowserReceiptScanAndSave(t *testing.T) {
	analyzer := browserReceiptAnalyzer{
		analysis: &domain.ReceiptAnalysis{
			Summary:       "LAWSON 超商",
			Total:         860,
			Currency:      domain.CurrencyJPY,
			Category:      domain.CategoryGroceries,
			PaymentMethod: domain.PaymentMethodIcCard,
			Items: []domain.ReceiptItem{
				{Name: "からあげクン", NameZH: "炸雞塊", Price: 260, Category: domain.CategoryFood},
				{Name: "お茶", NameZH: "綠茶", Price: 600, Category: domain.CategoryGroceries},
			},
		},
	}
	app := newBrowserApp(t, appOptions{analyzer: analyzer})
	app.open()
	app.waitReady()

	assert.False(t, app.flag(app.visible("#more-categories")), "secondary categories start collapsed")

	// Simulate picking a receipt photo in the file input.
	app.run(chromedp.Evaluate(`(() => {
		const input = document.getElementById("receipt-file-input");
		const dt = new DataTransfer();
		dt.items.add(new File(["fake-image"], "receipt.jpg", { type: "image/jpeg" }));
		input.files = dt.files;
		input.dispatchEvent(new Event("change", { bubbles: true }));
	})()`, nil))

	app.waitFor("the scanned summary", `document.getElementById("name").value === "LAWSON 超商"`)
	assert.Equal(t, "860", app.text(`document.getElementById("price").value`))
	assert.Equal(t, "JPY", app.text(`document.getElementById("currency-input").value`))
	assert.Equal(t, "groceries", app.text(`document.getElementById("category-input").value`))
	assert.Equal(t, "ic_card", app.text(`document.getElementById("method-input").value`))
	assert.True(t, app.flag(app.visible("#more-categories")), "selecting a secondary category expands the section")
	assert.Equal(t, "https://example.com/receipt.jpg", app.text(`document.getElementById("receipt-url").value`))

	app.click("#submit-btn")
	app.waitFor("the success toast", toastSaying("LAWSON 超商"))

	require.Equal(t, 1, app.expenseCount())
	saved := app.expenseAt(0)
	assert.Equal(t, "LAWSON 超商", saved.Name)
	assert.Equal(t, uint64(860), saved.Price)
	assert.Equal(t, domain.CurrencyJPY, saved.Currency)
	assert.Equal(t, domain.CategoryGroceries, saved.Category)
	assert.Equal(t, domain.PaymentMethodIcCard, saved.Method)
	assert.Equal(t, "https://example.com/receipt.jpg", saved.ReceiptURL)
	require.Len(t, saved.ReceiptItems, 2)
	assert.Equal(t, "からあげクン（炸雞塊）", saved.ReceiptItems[0].Name)
	assert.Equal(t, int64(260), saved.ReceiptItems[0].Price)

	app.requireNoProblems()
}

func TestBrowserForeignCurrencyAndNumpadMath(t *testing.T) {
	app := newBrowserApp(t, appOptions{})
	app.open()
	app.waitReady()

	// Switch to JPY on a TWD trip: rate section opens and fetches 0.22.
	app.click(`#currency-tabs [data-tui-tabs-value="JPY"]`)
	app.waitFor("the live exchange rate", `document.getElementById("exchange-rate-input").value === "0.22"`)
	assert.Equal(t, "(即時)", app.text(`document.getElementById("rate-status").textContent`))
	assert.Contains(t, app.text(`document.getElementById("rate-hint").textContent`), "1 JPY = NT$0.2200")

	// Open numpad, use "00" and "+" keys: 100 + 900 = 1000 JPY (≈ NT$220).
	app.click("#price")
	app.waitFor("the numpad", app.visible("#numpad-sheet"))
	app.click(`[data-numpad-key="1"]`)
	app.click(`[data-numpad-key="00"]`)
	app.click(`[data-numpad-key="+"]`)
	app.click(`[data-numpad-key="9"]`)
	app.click(`[data-numpad-key="00"]`)
	assert.Equal(t, "100+900 = 1,000", app.text(`document.getElementById("numpad-display").textContent`))
	assert.Equal(t, "≈ NT$220", app.text(`document.getElementById("numpad-convert").textContent`))
	assert.Equal(t, "1000", app.text(`document.getElementById("price").value`))
	app.click("#numpad-confirm")

	// Leave #name empty: submitting defaults the name to the chosen category label ("交通").
	app.click(`#category-tabs [data-tui-tabs-value="transport"]`)
	app.click("#submit-btn")
	app.waitFor("the success toast", toastSaying("交通"))

	require.Equal(t, 1, app.expenseCount())
	filed := app.expenseAt(0)
	assert.Equal(t, "交通", filed.Name)
	assert.Equal(t, uint64(1000), filed.Price)
	assert.Equal(t, domain.CurrencyJPY, filed.Currency)
	assert.Equal(t, "0.22", filed.ExchangeRate.String())

	// If /api/rates fails on refresh, the cached rate in localStorage is used.
	app.failRates.Store(true)
	app.run(chromedp.Evaluate(`document.getElementById("exchange-rate-input").value = ""`, nil))
	app.click("#fetch-rate-btn")
	app.waitFor("the cached rate fallback", `document.getElementById("rate-status").textContent.includes("快取")`)
	assert.Equal(t, "0.22", app.text(`document.getElementById("exchange-rate-input").value`))
}

func TestBrowserMultiTripSwitchAndScopedDefaults(t *testing.T) {
	app := newBrowserApp(t, appOptions{
		extraTrips: []domain.Trip{{ID: 20, Title: "2026 Osaka Food", Currency: domain.CurrencyJPY}},
	})
	app.open()
	app.waitReady()

	// On trip 10 (TWD), choose Bob (67890) as payer and TWD as currency, then file an expense.
	assert.Equal(t, "10", app.text(`document.body.dataset.tripId`))
	assert.Equal(t, "TWD", app.text(`document.getElementById("currency-input").value`))
	app.run(chromedp.Evaluate(`(() => {
		const sel = document.getElementById("paid-by-select");
		sel.value = "67890";
	})()`, nil))
	app.fillExpense("台北計程車", "250")
	app.click("#submit-btn")
	app.waitFor("the success toast", toastSaying("台北計程車"))

	// Switch to trip 20 (JPY); the page reloads onto trip 20 with its own currency and default payer.
	app.run(chromedp.Evaluate(`(() => {
		const sel = document.getElementById("trip-select");
		sel.value = "20";
		sel.dispatchEvent(new Event("change", { bubbles: true }));
	})()`, nil))
	app.waitFor("trip 20 to load", `document.body.dataset.tripId === "20" && document.body.dataset.appReady === "true"`)
	assert.Equal(t, "JPY", app.text(`document.getElementById("currency-input").value`), "trip 20 uses its own currency")
	assert.Equal(t, "12345", app.text(`document.getElementById("paid-by-select").value`), "trip 10's saved payer does not leak to trip 20")

	app.requireNoProblems()
}

func TestBrowserParticipantsValidationAndSplit(t *testing.T) {
	app := newBrowserApp(t, appOptions{})
	app.open()
	app.waitReady()

	app.fillExpense("個人咖啡", "150")

	// Uncheck all participants: submitting must be blocked with #participants-error.
	app.run(chromedp.Evaluate(`(() => {
		for (const box of document.querySelectorAll('#participants input[name="participants"]')) {
			box.checked = false;
		}
	})()`, nil))
	app.click("#submit-btn")
	app.waitFor("the participants error", app.visible("#participants-error"))
	assert.Equal(t, 0, app.expenseCount(), "no expense is filed with zero participants")

	// Check only Bob (9) and submit.
	app.run(chromedp.Evaluate(`(() => {
		const bob = document.querySelector('#participants input[value="9"]');
		if (bob) bob.checked = true;
	})()`, nil))
	app.click("#submit-btn")
	app.waitFor("the success toast", toastSaying("個人咖啡"))
	assert.False(t, app.flag(app.visible("#participants-error")))

	require.Equal(t, 1, app.expenseCount())
	assert.Equal(t, []string{"9"}, app.expenseAt(0).ParticipantIDs)

	app.requireNoProblems()
}

func TestBrowserStatisticsDrillDowns(t *testing.T) {
	app := newBrowserApp(t, appOptions{})
	app.repo.expenses = []domain.Expense{
		{
			ID: "exp-1", Name: "新幹線", Price: 3000, Currency: domain.CurrencyTWD,
			Category: domain.CategoryTransport, Method: domain.PaymentMethodCreditCard, PaidByID: "8",
			ShoppedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			ID: "exp-2", Name: "抹茶冰淇淋", Price: 200, Currency: domain.CurrencyTWD,
			Category: domain.CategoryFood, Method: domain.PaymentMethodIcCard, PaidByID: "9",
			ShoppedAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
		},
	}
	app.open()
	app.waitReady()

	// Drill down by payment method (credit_card).
	app.click("#nav-stats")
	app.waitFor("the statistics", app.visible(`button[data-list-method="credit_card"]`))
	app.click(`button[data-list-method="credit_card"]`)
	app.waitFor("the list filtered by method", `document.getElementById("list-content")?.innerText.includes("新幹線")`)
	assert.Equal(t, "credit_card", app.text(`document.getElementById("list-method").value`))
	assert.NotContains(t, app.text(`document.getElementById("list-content").innerText`), "抹茶冰淇淋")

	// Drill down by payer (Bob = "9").
	app.click("#nav-stats")
	app.waitFor("the statistics", app.visible(`button[data-list-payer="9"]`))
	app.click(`button[data-list-payer="9"]`)
	app.waitFor("the list filtered by payer", `document.getElementById("list-content")?.innerText.includes("抹茶冰淇淋")`)
	assert.Equal(t, "9", app.text(`document.getElementById("list-payer").value`))
	assert.NotContains(t, app.text(`document.getElementById("list-content").innerText`), "新幹線")

	// Drill down by date ("2026-10-01").
	app.click("#nav-stats")
	app.waitFor("the statistics", app.visible(`button[data-list-date="2026-10-01"]`))
	app.click(`button[data-list-date="2026-10-01"]`)
	app.waitFor("the list filtered by date", `document.getElementById("list-content")?.innerText.includes("新幹線")`)
	assert.Equal(t, "2026-10-01", app.text(`document.getElementById("list-date").value`))
	assert.NotContains(t, app.text(`document.getElementById("list-content").innerText`), "抹茶冰淇淋")

	app.requireNoProblems()
}
