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
	"time"
)

var (
	// ErrInvalidInitData is returned when the initData is invalid or tampered.
	ErrInvalidInitData = errors.New("invalid init data")
	// ErrExpiredInitData is returned when the initData is expired.
	ErrExpiredInitData = errors.New("expired init data")
	// ErrMissingHash is returned when the hash is missing from initData.
	ErrMissingHash = errors.New("missing hash in init data")
)

// TelegramInitData represents the parsed and validated Telegram WebApp init data.
type TelegramInitData struct {
	QueryID      string
	UserID       int64
	Username     string
	FirstName    string
	LastName     string
	LanguageCode string
	AuthDate     time.Time
	Hash         string
}

// ValidateTelegramInitData validates the Telegram WebApp initData string.
// It verifies the HMAC-SHA256 signature using the bot token.
//
// The validation follows Telegram's specification:
// 1. Parse the query string
// 2. Sort all key=value pairs alphabetically by key (excluding hash)
// 3. Join with newlines to create data_check_string
// 4. Create secret_key = HMAC-SHA256(bot_token, "WebAppData")
// 5. Verify hash = HMAC-SHA256(data_check_string, secret_key)
func ValidateTelegramInitData(initData, botToken string, maxAge time.Duration) (*TelegramInitData, error) {
	if initData == "" {
		return nil, ErrInvalidInitData
	}

	// Parse the query string
	values, err := url.ParseQuery(initData)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse query string", ErrInvalidInitData)
	}

	// Extract and remove hash
	hash := values.Get("hash")
	if hash == "" {
		return nil, ErrMissingHash
	}
	values.Del("hash")

	// Build data_check_string: sort keys alphabetically and join with newlines
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

	// Create secret key: HMAC-SHA256("WebAppData", bot_token)
	secretKeyHMAC := hmac.New(sha256.New, []byte("WebAppData"))
	secretKeyHMAC.Write([]byte(botToken))
	secretKey := secretKeyHMAC.Sum(nil)

	// Calculate expected hash: HMAC-SHA256(data_check_string, secret_key)
	expectedHashHMAC := hmac.New(sha256.New, secretKey)
	expectedHashHMAC.Write([]byte(dataCheckString))
	expectedHash := hex.EncodeToString(expectedHashHMAC.Sum(nil))

	// Compare hashes
	if !hmac.Equal([]byte(hash), []byte(expectedHash)) {
		return nil, ErrInvalidInitData
	}

	// Parse auth_date and check expiry
	authDateStr := values.Get("auth_date")
	authDateUnix, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid auth_date", ErrInvalidInitData)
	}
	authDate := time.Unix(authDateUnix, 0)

	if maxAge > 0 && time.Since(authDate) > maxAge {
		return nil, ErrExpiredInitData
	}

	// Parse user data (it's a JSON string)
	result := &TelegramInitData{
		QueryID:  values.Get("query_id"),
		AuthDate: authDate,
		Hash:     hash,
	}

	// Parse user JSON if present
	userJSON := values.Get("user")
	if userJSON != "" {
		if err := parseUserJSON(userJSON, result); err != nil {
			return nil, fmt.Errorf("%w: failed to parse user data", ErrInvalidInitData)
		}
	}

	return result, nil
}

// parseUserJSON parses the user JSON string from initData.
func parseUserJSON(userJSON string, data *TelegramInitData) error {
	// The user field is a URL-encoded JSON object
	// We need to parse it manually to extract user info

	// Simple JSON parsing without importing encoding/json to keep it lightweight
	// Format: {"id":123,"first_name":"John",...}

	// Find id
	if idx := strings.Index(userJSON, `"id":`); idx != -1 {
		start := idx + 5
		end := start
		for end < len(userJSON) && userJSON[end] >= '0' && userJSON[end] <= '9' {
			end++
		}
		if end > start {
			id, err := strconv.ParseInt(userJSON[start:end], 10, 64)
			if err == nil {
				data.UserID = id
			}
		}
	}

	// Find username
	data.Username = extractJSONString(userJSON, "username")
	data.FirstName = extractJSONString(userJSON, "first_name")
	data.LastName = extractJSONString(userJSON, "last_name")
	data.LanguageCode = extractJSONString(userJSON, "language_code")

	return nil
}

// extractJSONString extracts a string value from a simple JSON object.
func extractJSONString(json, key string) string {
	search := fmt.Sprintf(`"%s":"`, key)
	idx := strings.Index(json, search)
	if idx == -1 {
		return ""
	}

	start := idx + len(search)
	end := start

	// Find the closing quote, handling escaped quotes
	for end < len(json) {
		if json[end] == '"' && (end == start || json[end-1] != '\\') {
			break
		}
		end++
	}

	if end > start && end < len(json) {
		return json[start:end]
	}
	return ""
}
