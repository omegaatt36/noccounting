package integration_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omegaatt36/noccounting/domain"
)

func TestBrowserLogAndReviewAnExpense(t *testing.T) {
	app := newBrowserApp(t, appOptions{})
	app.open()
	app.waitReady()

	// The dashboard is opened while nothing is spent, then an expense is filed:
	// opening it again must show that expense, not the draw from before.
	app.click("#nav-dashboard")
	app.waitFor("the empty dashboard", `document.getElementById("dashboard-content")?.innerText.includes("0 筆")`)
	app.waitFor("the empty-state call to action", app.visible(`[data-action="show-form"]`))
	app.click(`[data-action="show-form"]`)
	app.waitFor("the form", app.visible("#expense-form"))

	app.fillExpense("東京拉麵", "1200")
	assert.Equal(t, "1200", app.text(`document.getElementById("price").value`))
	app.click("#submit-btn")
	app.waitFor("the success toast", `document.querySelector("#result [data-tui-toast]")?.textContent.includes("東京拉麵")`)

	require.Equal(t, 1, app.expenseCount())
	app.repo.mu.Lock()
	filed := app.repo.expenses[0]
	app.repo.mu.Unlock()
	assert.Equal(t, "東京拉麵", filed.Name)
	assert.Equal(t, uint64(1200), filed.Price)
	assert.Equal(t, domain.CurrencyTWD, filed.Currency)
	assert.Equal(t, "8", filed.PaidByID)
	assert.Equal(t, "", app.text(`document.getElementById("name").value`), "the form is cleared for the next expense")

	app.click("#nav-dashboard")
	app.waitFor("the new expense on the dashboard", `document.getElementById("dashboard-content")?.innerText.includes("1 筆")`)

	foodRow := `button[data-detail="cat-detail-food"]`
	app.click(foodRow)
	app.waitFor("the category's expenses", `document.getElementById("cat-detail-food")?.innerText.includes("東京拉麵")`)
	assert.Equal(t, "true", app.text(`document.querySelector('`+foodRow+`').getAttribute("aria-expanded")`))

	app.click(foodRow)
	app.waitFor("the category to close", `document.getElementById("cat-detail-food")?.children.length === 0`)
	assert.Equal(t, "false", app.text(`document.querySelector('`+foodRow+`').getAttribute("aria-expanded")`))

	app.click(foodRow)
	app.waitFor("the category's expenses again", `document.getElementById("cat-detail-food")?.innerText.includes("東京拉麵")`)
	app.click(`#cat-detail-food button[aria-label^="刪除"]`)
	app.waitFor("the dashboard to redraw empty", `document.getElementById("dashboard-content")?.innerText.includes("0 筆")`)
	assert.Equal(t, 0, app.expenseCount())

	// A page that leaves its console clean ran under the CSP without a single
	// blocked script, style, or request.
	app.requireNoProblems()
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

func TestBrowserDashboardFailureOffersRetry(t *testing.T) {
	app := newBrowserApp(t, appOptions{})
	app.open()
	app.waitReady()

	app.repo.mu.Lock()
	app.repo.queryErr = errors.New("TREK is down")
	app.repo.mu.Unlock()

	app.click("#nav-dashboard")
	app.waitFor("the dashboard error", app.visible("#dashboard-error"))

	app.repo.mu.Lock()
	app.repo.queryErr = nil
	app.repo.mu.Unlock()

	app.click(`[data-action="retry-dashboard"]`)
	app.waitFor("the dashboard", `document.getElementById("dashboard-content")?.innerText.includes("0 筆")`)
	assert.False(t, app.flag(app.visible("#dashboard-error")), "a recovered dashboard drops the error")
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

		app.click("#nav-dashboard")
		assert.False(t, app.flag(`window.__tg.mainButton.isVisible`), "the dashboard has no form to file")
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
		app.click("#nav-dashboard")
		assert.Equal(t, "page", app.text(`document.getElementById("nav-dashboard").getAttribute("aria-current")`))
		assert.False(t, app.flag(`document.getElementById("nav-form").hasAttribute("aria-current")`))
	})

	app.requireNoProblems()
}
