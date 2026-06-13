package webapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// telegramUser represents the user object embedded in Telegram initData.
type telegramUser struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	LanguageCode string `json:"language_code"`
}

// parseUserJSON parses the user JSON string from initData using encoding/json.
func parseUserJSON(userJSON string, data *TelegramInitData) error {
	var user telegramUser
	if err := json.Unmarshal([]byte(userJSON), &user); err != nil {
		return err
	}

	data.UserID = user.ID
	data.Username = user.Username
	data.FirstName = user.FirstName
	data.LastName = user.LastName
	data.LanguageCode = user.LanguageCode

	return nil
}
