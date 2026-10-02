package trek

import (
	"strings"
	"testing"

	"github.com/omegaatt36/noccounting/domain"
)

func TestEncodeNote_PutsTheMethodBehindThePrefix(t *testing.T) {
	for _, method := range domain.PaymentMethodValues() {
		t.Run(string(method), func(t *testing.T) {
			if got, want := encodeNote(method, ""), "PAYMENT:"+string(method); got != want {
				t.Errorf("encodeNote(%s, \"\") = %q, want %q", method, got, want)
			}
			if got, want := encodeNote(method, "lunch"), "PAYMENT:"+string(method)+"|lunch"; got != want {
				t.Errorf("encodeNote(%s, lunch) = %q, want %q", method, got, want)
			}
		})
	}
}

func TestEncodeNote_OmitsThePrefixWhenThereIsNoMethodToRecord(t *testing.T) {
	tests := []struct {
		name   string
		method domain.PaymentMethod
		text   string
		want   string
	}{
		{name: "no method and no text", method: "", text: "", want: ""},
		{name: "no method, plain text", method: "", text: "lunch", want: "lunch"},
		{name: "a method from a newer version", method: "crypto", text: "lunch", want: "lunch"},
		{name: "a method carrying whitespace", method: " cash ", text: "lunch", want: "lunch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := encodeNote(tt.method, tt.text); got != tt.want {
				t.Errorf("encodeNote(%q, %q) = %q, want %q", tt.method, tt.text, got, tt.want)
			}
		})
	}
}

func TestDecodeNote_ReadsBackAnEncodedNote(t *testing.T) {
	tests := []struct {
		name       string
		note       string
		wantMethod domain.PaymentMethod
		wantText   string
	}{
		{name: "a method and nothing else", note: "PAYMENT:cash", wantMethod: domain.PaymentMethodCash},
		{name: "a method and text", note: "PAYMENT:e_pay|ramen", wantMethod: domain.PaymentMethodEPay, wantText: "ramen"},
		{name: "a method and empty text", note: "PAYMENT:ic_card|", wantMethod: domain.PaymentMethodIcCard},
		{name: "text that is only whitespace", note: "PAYMENT:credit_card|  ", wantMethod: domain.PaymentMethodCreditCard, wantText: "  "},
		{name: "a method a user padded by hand", note: "PAYMENT: cash |ramen", wantMethod: domain.PaymentMethodCash, wantText: "ramen"},
		{name: "a multi-line note", note: "PAYMENT:cash|ramen\nbeer", wantMethod: domain.PaymentMethodCash, wantText: "ramen\nbeer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method, text := decodeNote(tt.note)
			if method != tt.wantMethod {
				t.Errorf("decodeNote(%q) method = %q, want %q", tt.note, method, tt.wantMethod)
			}
			if text != tt.wantText {
				t.Errorf("decodeNote(%q) text = %q, want %q", tt.note, text, tt.wantText)
			}
		})
	}
}

func TestDecodeNote_LeavesANoteItDidNotWriteAlone(t *testing.T) {
	tests := []struct {
		name     string
		note     string
		wantText string
	}{
		{name: "an absent note", note: ""},
		{name: "a note TREK's own UI wrote", note: "team dinner", wantText: "team dinner"},
		{name: "a note carrying TREK's own ticket prefix", note: `TICKETJSON:{"items":[]}`, wantText: `TICKETJSON:{"items":[]}`},
		{name: "the prefix and nothing after it", note: "PAYMENT:", wantText: "PAYMENT:"},
		{name: "a method this version does not know", note: "PAYMENT:crypto|ramen", wantText: "PAYMENT:crypto|ramen"},
		{name: "a user who typed the prefix themselves", note: "PAYMENT:ask me|", wantText: "PAYMENT:ask me|"},
		{name: "a prefix that merely contains ours", note: "XPAYMENT:cash", wantText: "XPAYMENT:cash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method, text := decodeNote(tt.note)
			if method != "" {
				t.Errorf("decodeNote(%q) method = %q, want no method", tt.note, method)
			}
			if text != tt.wantText {
				t.Errorf("decodeNote(%q) text = %q, want %q", tt.note, text, tt.wantText)
			}
		})
	}
}

func TestNoteRoundTrip_KeepsFreeTextThatContainsTheSeparator(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{name: "one separator", text: "ramen|beer"},
		{name: "several separators", text: "a|b|c"},
		{name: "a separator on the boundary", text: "|ramen|"},
		{name: "text that starts with the prefix", text: "PAYMENT: not a method"},
		{name: "nothing but separators", text: "||"},
		{name: "padding around the text", text: "  ramen  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			note := encodeNote(domain.PaymentMethodCreditCard, tt.text)

			method, text := decodeNote(note)
			if method != domain.PaymentMethodCreditCard {
				t.Errorf("decodeNote(%q) method = %q, want %q", note, method, domain.PaymentMethodCreditCard)
			}
			if text != tt.text {
				t.Errorf("decodeNote(%q) text = %q, want %q", note, text, tt.text)
			}
		})
	}
}

func TestNoteRoundTrip_DoesNotCollideWithTREKsTicketPrefix(t *testing.T) {
	const trekTicketPrefix = "TICKETJSON:"

	if strings.HasPrefix(paymentNotePrefix, trekTicketPrefix) || strings.HasPrefix(trekTicketPrefix, paymentNotePrefix) {
		t.Fatalf("%q and %q overlap, so one reader would claim the other's notes", paymentNotePrefix, trekTicketPrefix)
	}

	ticket := `TICKETJSON:{"items":[{"name":"ramen","price":500}]}`
	note := encodeNote(domain.PaymentMethodCash, ticket)

	if strings.HasPrefix(note, trekTicketPrefix) {
		t.Errorf("encodeNote(%q) = %q, which TREK would read as its own receipt payload", ticket, note)
	}
	method, text := decodeNote(note)
	if method != domain.PaymentMethodCash {
		t.Errorf("decodeNote(%q) method = %q, want %q", note, method, domain.PaymentMethodCash)
	}
	if text != ticket {
		t.Errorf("decodeNote(%q) text = %q, want the payload back uncut", note, text)
	}
}

func TestNoteRoundTrip_ReEncodesTheMethodOnUpdate(t *testing.T) {
	const stored = "PAYMENT:cash|night market"

	method, text := decodeNote(stored)
	if method != domain.PaymentMethodCash {
		t.Fatalf("decodeNote(%q) method = %q, want %q", stored, method, domain.PaymentMethodCash)
	}

	updated := encodeNote(domain.PaymentMethodEPay, text)
	if want := "PAYMENT:e_pay|night market"; updated != want {
		t.Errorf("re-encoded note = %q, want %q", updated, want)
	}

	back, backText := decodeNote(updated)
	if back != domain.PaymentMethodEPay || backText != text {
		t.Errorf("decodeNote(%q) = (%q, %q), want (%q, %q)", updated, back, backText, domain.PaymentMethodEPay, text)
	}
}
