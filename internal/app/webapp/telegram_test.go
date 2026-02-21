package webapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestValidateTelegramInitDataValid tests that valid, properly signed initData is accepted.
func TestValidateTelegramInitDataValid(t *testing.T) {
	botToken := "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz"
	maxAge := 5 * time.Minute

	params := map[string]string{
		"query_id": "AAHdF6IQAAAAAAAA",
		"user":     `{"id":123456789,"is_bot":false,"first_name":"John","last_name":"Doe","username":"johndoe","language_code":"en"}`,
	}

	initData := buildValidTelegramInitData(botToken, params)

	result, err := ValidateTelegramInitData(initData, botToken, maxAge)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result == nil {
		t.Fatalf("expected non-nil result")
	}

	if result.QueryID != "AAHdF6IQAAAAAAAA" {
		t.Errorf("expected QueryID 'AAHdF6IQAAAAAAAA', got %s", result.QueryID)
	}

	if result.UserID != 123456789 {
		t.Errorf("expected UserID 123456789, got %d", result.UserID)
	}

	if result.FirstName != "John" {
		t.Errorf("expected FirstName 'John', got %s", result.FirstName)
	}

	if result.LastName != "Doe" {
		t.Errorf("expected LastName 'Doe', got %s", result.LastName)
	}

	if result.Username != "johndoe" {
		t.Errorf("expected Username 'johndoe', got %s", result.Username)
	}

	if result.LanguageCode != "en" {
		t.Errorf("expected LanguageCode 'en', got %s", result.LanguageCode)
	}
}

// TestValidateTelegramInitDataInvalidHash tests that invalid hash is rejected.
func TestValidateTelegramInitDataInvalidHash(t *testing.T) {
	botToken := "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz"
	maxAge := 5 * time.Minute

	params := map[string]string{
		"query_id": "AAHdF6IQAAAAAAAA",
		"user":     `{"id":123456789,"is_bot":false,"first_name":"John"}`,
	}

	initData := buildValidTelegramInitData(botToken, params)
	// Tamper with the hash
	values, _ := url.ParseQuery(initData)
	values.Set("hash", "tampered0000000000000000000000000000000000000000000000000000")

	tamperedInitData := values.Encode()

	_, err := ValidateTelegramInitData(tamperedInitData, botToken, maxAge)
	if err == nil {
		t.Fatalf("expected error for invalid hash")
	}

	if !errors.Is(err, ErrInvalidInitData) {
		t.Errorf("expected ErrInvalidInitData, got %v", err)
	}
}

// TestValidateTelegramInitDataExpired tests that expired auth_date is rejected.
func TestValidateTelegramInitDataExpired(t *testing.T) {
	botToken := "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz"
	maxAge := 1 * time.Second

	// Create initData with very old auth_date (15 seconds ago)
	oldTime := time.Now().Add(-15 * time.Second).Unix()
	params := map[string]string{
		"query_id":  "AAHdF6IQAAAAAAAA",
		"user":      `{"id":123456789,"is_bot":false,"first_name":"John"}`,
		"auth_date": strconv.FormatInt(oldTime, 10),
	}

	initData := buildValidTelegramInitData(botToken, params)

	_, err := ValidateTelegramInitData(initData, botToken, maxAge)
	if err == nil {
		t.Fatalf("expected error for expired initData")
	}

	if !errors.Is(err, ErrExpiredInitData) {
		t.Errorf("expected ErrExpiredInitData, got %v", err)
	}
}

// TestValidateTelegramInitDataMissingHash tests that missing hash is rejected.
func TestValidateTelegramInitDataMissingHash(t *testing.T) {
	botToken := "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz"
	maxAge := 5 * time.Minute

	// Create initData without hash
	params := url.Values{}
	params.Set("query_id", "AAHdF6IQAAAAAAAA")
	params.Set("auth_date", strconv.FormatInt(time.Now().Unix(), 10))

	_, err := ValidateTelegramInitData(params.Encode(), botToken, maxAge)
	if err == nil {
		t.Fatalf("expected error for missing hash")
	}

	if !errors.Is(err, ErrMissingHash) {
		t.Errorf("expected ErrMissingHash, got %v", err)
	}
}

// TestValidateTelegramInitDataEmptyString tests that empty initData is rejected.
func TestValidateTelegramInitDataEmptyString(t *testing.T) {
	botToken := "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz"
	maxAge := 5 * time.Minute

	_, err := ValidateTelegramInitData("", botToken, maxAge)
	if err == nil {
		t.Fatalf("expected error for empty initData")
	}

	if !errors.Is(err, ErrInvalidInitData) {
		t.Errorf("expected ErrInvalidInitData, got %v", err)
	}
}

// TestValidateTelegramInitDataInvalidAuthDate tests that invalid auth_date format is rejected.
func TestValidateTelegramInitDataInvalidAuthDate(t *testing.T) {
	botToken := "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz"
	maxAge := 5 * time.Minute

	params := map[string]string{
		"query_id":  "AAHdF6IQAAAAAAAA",
		"auth_date": "not_a_number",
	}

	initData := buildValidTelegramInitData(botToken, params)

	_, err := ValidateTelegramInitData(initData, botToken, maxAge)
	if err == nil {
		t.Fatalf("expected error for invalid auth_date")
	}

	if !errors.Is(err, ErrInvalidInitData) {
		t.Errorf("expected ErrInvalidInitData, got %v", err)
	}
}

// TestValidateTelegramInitDataWrongToken tests that wrong bot token is rejected.
func TestValidateTelegramInitDataWrongToken(t *testing.T) {
	botToken := "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz"
	wrongToken := "different_token_654321"
	maxAge := 5 * time.Minute

	params := map[string]string{
		"query_id": "AAHdF6IQAAAAAAAA",
		"user":     `{"id":123456789,"is_bot":false,"first_name":"John"}`,
	}

	initData := buildValidTelegramInitData(botToken, params)

	// Try to validate with wrong token
	_, err := ValidateTelegramInitData(initData, wrongToken, maxAge)
	if err == nil {
		t.Fatalf("expected error when validating with wrong token")
	}

	if !errors.Is(err, ErrInvalidInitData) {
		t.Errorf("expected ErrInvalidInitData, got %v", err)
	}
}

// TestValidateTelegramInitDataNoMaxAge tests that maxAge=0 disables expiration check.
func TestValidateTelegramInitDataNoMaxAge(t *testing.T) {
	botToken := "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz"

	// Create initData with very old auth_date
	oldTime := time.Now().Add(-1 * time.Hour).Unix()
	params := map[string]string{
		"query_id":  "AAHdF6IQAAAAAAAA",
		"auth_date": strconv.FormatInt(oldTime, 10),
	}

	initData := buildValidTelegramInitData(botToken, params)

	// With maxAge=0, expiration check should be disabled
	result, err := ValidateTelegramInitData(initData, botToken, 0)
	if err != nil {
		t.Fatalf("expected no error with maxAge=0, got %v", err)
	}

	if result == nil {
		t.Fatalf("expected non-nil result")
	}
}

func TestExtractJSONString(t *testing.T) {
	tests := []struct {
		json     string
		key      string
		expected string
	}{
		{`{"username":"john"}`, "username", "john"},
		{`{"id":123,"username":"john","first_name":"John"}`, "username", "john"},
		{`{"first_name":"John Doe"}`, "first_name", "John Doe"},
		{`{"username":""}`, "username", ""},
		{`{"id":123}`, "username", ""},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := extractJSONString(tt.json, tt.key)
			if result != tt.expected {
				t.Errorf("extractJSONString(%q, %q) = %q, want %q", tt.json, tt.key, result, tt.expected)
			}
		})
	}
}

// buildValidTelegramInitData constructs a valid Telegram initData string with proper HMAC signature.
// Returns the initData query string.
func buildValidTelegramInitData(botToken string, params map[string]string) string {
	// Create secret key: HMAC-SHA256("WebAppData", bot_token)
	secretKeyHMAC := hmac.New(sha256.New, []byte("WebAppData"))
	secretKeyHMAC.Write([]byte(botToken))
	secretKey := secretKeyHMAC.Sum(nil)

	// Set auth_date if not provided
	if _, ok := params["auth_date"]; !ok {
		params["auth_date"] = strconv.FormatInt(time.Now().Unix(), 10)
	}

	// Build data_check_string: sort keys alphabetically (excluding hash) and join with newlines
	var keys []string
	for key := range params {
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var dataCheckParts []string
	for _, key := range keys {
		dataCheckParts = append(dataCheckParts, fmt.Sprintf("%s=%s", key, params[key]))
	}
	dataCheckString := strings.Join(dataCheckParts, "\n")

	// Calculate hash: HMAC-SHA256(data_check_string, secret_key)
	hashHMAC := hmac.New(sha256.New, secretKey)
	hashHMAC.Write([]byte(dataCheckString))
	hash := hex.EncodeToString(hashHMAC.Sum(nil))

	// Add hash to params
	params["hash"] = hash

	// Build query string
	values := url.Values{}
	for key, val := range params {
		values.Set(key, val)
	}
	return values.Encode()
}
