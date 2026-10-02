package trek

import (
	"strings"

	"github.com/omegaatt36/noccounting/domain"
)

// TREK migrates notes beginning TICKETJSON: into ticket_json; payment notes use a separate prefix.
const (
	paymentNotePrefix    = "PAYMENT:"
	paymentNoteSeparator = "|"
)

func encodeNote(method domain.PaymentMethod, text string) string {
	if !method.IsValid() {
		return text
	}
	if text == "" {
		return paymentNotePrefix + string(method)
	}
	return paymentNotePrefix + string(method) + paymentNoteSeparator + text
}

func decodeNote(note string) (domain.PaymentMethod, string) {
	if !strings.HasPrefix(note, paymentNotePrefix) {
		return "", note
	}

	token, text, _ := strings.Cut(strings.TrimPrefix(note, paymentNotePrefix), paymentNoteSeparator)
	method, err := domain.ParsePaymentMethod(strings.TrimSpace(token))
	if err != nil {
		return "", note
	}
	return method, text
}
