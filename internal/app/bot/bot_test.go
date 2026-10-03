package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tele "gopkg.in/telebot.v4"
)

func TestNew_RejectsInvalidPollerConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"webhook without public URL", Config{Token: "t", Poller: PollerWebhook, WebhookSecretToken: "s"}},
		{"webhook without secret token", Config{Token: "t", Poller: PollerWebhook, WebhookPublicURL: "https://example.com/x"}},
		{"unknown poller", Config{Token: "t", Poller: "carrier-pigeon"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// These are refused before the bot reaches Telegram, so no token is needed.
			if _, err := New(tt.cfg, nil, nil, nil); err == nil {
				t.Fatal("New accepted a config that cannot work")
			}
		})
	}
}

func TestCommandMenu_IsRegisteredWithTelegram(t *testing.T) {
	var path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		path, body = r.URL.Path, string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	t.Cleanup(srv.Close)

	bot, err := tele.NewBot(tele.Settings{Token: "test-token", URL: srv.URL, Offline: true, Synchronous: true})
	if err != nil {
		t.Fatalf("building an offline bot: %v", err)
	}

	if err := bot.SetCommands(commands); err != nil {
		t.Fatalf("SetCommands: %v", err)
	}

	if !strings.HasSuffix(path, "/setMyCommands") {
		t.Fatalf("the menu went to %q, want setMyCommands", path)
	}
	var req struct {
		Commands []tele.Command `json:"commands"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decoding the request: %v", err)
	}
	if len(req.Commands) != len(commands) {
		t.Fatalf("sent %d commands, want %d", len(req.Commands), len(commands))
	}
}

// TestCommandMenu_MatchesHelp keeps the menu and /help describing the same
// commands: a command in one and not the other is either unreachable from the
// menu or invisible in /help.
func TestCommandMenu_MatchesHelp(t *testing.T) {
	handler := newTestHandler(&spyAccountingRepo{}, &stubReceiptAnalyzer{})
	ctx := &spyContext{}
	if err := handler.handleHelp(ctx); err != nil {
		t.Fatalf("handleHelp: %v", err)
	}
	var help strings.Builder
	for _, msg := range ctx.sentMsgs {
		help.WriteString(msg.(string))
	}

	for _, command := range commands {
		switch command.Text {
		case "start", "help", "cancel": // not listed in /help's body
			continue
		}
		if !strings.Contains(help.String(), "/"+command.Text) {
			t.Errorf("/%s is in the menu but /help does not describe it", command.Text)
		}
	}
}
