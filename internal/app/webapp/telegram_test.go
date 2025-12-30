package webapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestValidateTelegramInitData(t *testing.T) {
	botToken := "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz"

	t.Run("valid init data", func(t *testing.T) {
		initData := createTestInitData(botToken, time.Now().Unix(), 12345, "testuser")

		result, err := ValidateTelegramInitData(initData, botToken, time.Hour)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if result.UserID != 12345 {
			t.Errorf("expected UserID 12345, got %d", result.UserID)
		}
		if result.Username != "testuser" {
			t.Errorf("expected Username 'testuser', got %s", result.Username)
		}
	})

	t.Run("invalid hash", func(t *testing.T) {
		initData := createTestInitData(botToken, time.Now().Unix(), 12345, "testuser")
		// Tamper with the hash
		initData = strings.Replace(initData, "hash=", "hash=invalid", 1)

		_, err := ValidateTelegramInitData(initData, botToken, time.Hour)
		if err != ErrInvalidInitData {
			t.Errorf("expected ErrInvalidInitData, got: %v", err)
		}
	})

	t.Run("wrong bot token", func(t *testing.T) {
		initData := createTestInitData(botToken, time.Now().Unix(), 12345, "testuser")

		_, err := ValidateTelegramInitData(initData, "wrong_token", time.Hour)
		if err != ErrInvalidInitData {
			t.Errorf("expected ErrInvalidInitData, got: %v", err)
		}
	})

	t.Run("expired init data", func(t *testing.T) {
		// Create init data from 2 hours ago
		initData := createTestInitData(botToken, time.Now().Add(-2*time.Hour).Unix(), 12345, "testuser")

		_, err := ValidateTelegramInitData(initData, botToken, time.Hour)
		if err != ErrExpiredInitData {
			t.Errorf("expected ErrExpiredInitData, got: %v", err)
		}
	})

	t.Run("no expiry check when maxAge is 0", func(t *testing.T) {
		// Create init data from 24 hours ago
		initData := createTestInitData(botToken, time.Now().Add(-24*time.Hour).Unix(), 12345, "testuser")

		_, err := ValidateTelegramInitData(initData, botToken, 0)
		if err != nil {
			t.Errorf("expected no error with maxAge=0, got: %v", err)
		}
	})

	t.Run("empty init data", func(t *testing.T) {
		_, err := ValidateTelegramInitData("", botToken, time.Hour)
		if err != ErrInvalidInitData {
			t.Errorf("expected ErrInvalidInitData, got: %v", err)
		}
	})

	t.Run("missing hash", func(t *testing.T) {
		initData := "user=%7B%22id%22%3A12345%7D&auth_date=1234567890"

		_, err := ValidateTelegramInitData(initData, botToken, time.Hour)
		if err != ErrMissingHash {
			t.Errorf("expected ErrMissingHash, got: %v", err)
		}
	})
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

// createTestInitData creates a valid initData string for testing.
func createTestInitData(botToken string, authDate int64, userID int64, username string) string {
	userJSON := fmt.Sprintf(`{"id":%d,"username":"%s","first_name":"Test","last_name":"User"}`, userID, username)

	values := url.Values{}
	values.Set("user", userJSON)
	values.Set("auth_date", fmt.Sprintf("%d", authDate))
	values.Set("query_id", "test_query_id")

	// Build data_check_string
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var dataCheckParts []string
	for _, key := range keys {
		dataCheckParts = append(dataCheckParts, fmt.Sprintf("%s=%s", key, values.Get(key)))
	}
	dataCheckString := strings.Join(dataCheckParts, "\n")

	// Create secret key
	secretKeyHMAC := hmac.New(sha256.New, []byte("WebAppData"))
	secretKeyHMAC.Write([]byte(botToken))
	secretKey := secretKeyHMAC.Sum(nil)

	// Calculate hash
	hashHMAC := hmac.New(sha256.New, secretKey)
	hashHMAC.Write([]byte(dataCheckString))
	hash := hex.EncodeToString(hashHMAC.Sum(nil))

	values.Set("hash", hash)

	return values.Encode()
}
