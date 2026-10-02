package trek

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/omegaatt36/noccounting/domain"
)

const (
	defaultTimeout = 30 * time.Second

	tripPathPrefix = "/api/trips"
)

var ErrSecondFactorRequired = errors.New("the TREK account requires two-factor authentication; a service account without 2FA is required")

type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	if e.Code != "" {
		return fmt.Sprintf("trek API error (status %d, %s): %s", e.StatusCode, e.Code, msg)
	}
	return fmt.Sprintf("trek API error (status %d): %s", e.StatusCode, msg)
}

type loginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	RememberMe bool   `json:"remember_me"`
}

type loginResponse struct {
	Token       string `json:"token"`
	MFARequired bool   `json:"mfa_required"`
}

type tripsResponse struct {
	Trips []tripEntry `json:"trips"`
}

type tripEntry struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	Currency  string  `json:"currency"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
}

type Client struct {
	httpClient *http.Client
	baseURL    string
	email      string
	password   string

	mu    sync.Mutex
	token string
}

func NewClient(cfg Config) *Client {
	return NewClientWithBaseURL(cfg, cfg.BaseURL)
}

func NewClientWithBaseURL(cfg Config, baseURL string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    strings.TrimRight(baseURL, "/"),
		email:      cfg.Email,
		password:   cfg.Password,
	}
}

func (c *Client) Start(ctx context.Context) error {
	return c.login(ctx)
}

func (c *Client) setToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

func (c *Client) sessionToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

// Login failures indicate invalid credentials and must not trigger session retries.
func (c *Client) login(ctx context.Context) error {
	body := loginRequest{Email: c.email, Password: c.password, RememberMe: true}

	respBody, status, err := c.raw(ctx, http.MethodPost, "/api/auth/login", body, "")
	if err != nil {
		return fmt.Errorf("trek login failed: %w", err)
	}
	if status != http.StatusOK {
		return newAPIError(status, respBody)
	}

	var resp loginResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return fmt.Errorf("trek login response: %w", err)
	}
	if resp.MFARequired {
		return ErrSecondFactorRequired
	}
	if resp.Token == "" {
		return fmt.Errorf("trek login returned no session token")
	}

	c.setToken(resp.Token)
	slog.Info("authenticated against TREK")
	return nil
}

func (c *Client) ListTrips(ctx context.Context) ([]domain.Trip, error) {
	var resp tripsResponse
	if err := c.do(ctx, http.MethodGet, tripPathPrefix, nil, &resp); err != nil {
		return nil, fmt.Errorf("trek trip list: %w", err)
	}

	trips := make([]domain.Trip, 0, len(resp.Trips))
	for _, entry := range resp.Trips {
		currency, err := domain.ParseCurrency(strings.ToUpper(strings.TrimSpace(entry.Currency)))
		if err != nil {
			slog.Warn("leaving out a TREK trip denominated in a currency noccounting cannot account in; the supported ones are TWD and JPY",
				"trip_id", entry.ID, "title", entry.Title, "currency", entry.Currency)
			continue
		}

		trips = append(trips, domain.Trip{
			ID:        entry.ID,
			Title:     entry.Title,
			Currency:  currency,
			StartDate: parseTripDay(entry.StartDate),
			EndDate:   parseTripDay(entry.EndDate),
		})
	}
	return trips, nil
}

func parseTripDay(raw *string) time.Time {
	if raw == nil {
		return time.Time{}
	}
	day, err := time.Parse(time.DateOnly, *raw)
	if err != nil {
		return time.Time{}
	}
	return day
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	return c.call(ctx, method, path, func(token string) ([]byte, int, error) {
		return c.raw(ctx, method, path, in, token)
	}, out)
}

func (c *Client) call(ctx context.Context, method, path string, send func(token string) ([]byte, int, error), out any) error {
	didRetry := false
	for {
		respBody, status, err := send(c.sessionToken())
		if err != nil {
			return err
		}

		if status == http.StatusUnauthorized && !didRetry {
			didRetry = true
			slog.Warn("TREK rejected the session token; re-authenticating once", "method", method, "path", path)
			if err := c.login(ctx); err != nil {
				return fmt.Errorf("trek re-authentication after %s %s: %w", method, path, err)
			}
			continue
		}

		if status < 200 || status >= 300 {
			return newAPIError(status, respBody)
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("trek %s %s response: %w", method, path, err)
		}
		return nil
	}
}

func (c *Client) raw(ctx context.Context, method, path string, in any, token string) ([]byte, int, error) {
	var reqBody io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return nil, 0, fmt.Errorf("trek request body: %w", err)
		}
		reqBody = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("trek request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("trek %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("trek %s %s response: %w", method, path, err)
	}
	return respBody, resp.StatusCode, nil
}

// Rewind before retrying: the first HTTP attempt consumes the file stream.
func (c *Client) postMultipart(ctx context.Context, path, fieldName, filename string, content io.ReadSeeker, contentType string, out any) error {
	return c.call(ctx, http.MethodPost, path, func(token string) ([]byte, int, error) {
		if _, err := content.Seek(0, io.SeekStart); err != nil {
			return nil, 0, fmt.Errorf("trek %s %s: rewinding %s: %w", http.MethodPost, path, filename, err)
		}
		return c.sendMultipart(ctx, path, fieldName, filename, content, contentType, token)
	}, out)
}

// Buffer multipart content so TREK receives an exact Content-Length.
func (c *Client) sendMultipart(ctx context.Context, path, fieldName, filename string, content io.Reader, contentType, token string) ([]byte, int, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	disposition := mime.FormatMediaType("form-data", map[string]string{
		"name":     fieldName,
		"filename": filename,
	})
	if disposition == "" {
		return nil, 0, fmt.Errorf("trek %s %s: the file name %q cannot be sent in a multipart header",
			http.MethodPost, path, filename)
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", disposition)
	header.Set("Content-Type", contentType)

	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, 0, fmt.Errorf("trek %s %s: %w", http.MethodPost, path, err)
	}
	if _, err := io.Copy(part, content); err != nil {
		return nil, 0, fmt.Errorf("trek %s %s: reading the file to upload: %w", http.MethodPost, path, err)
	}
	if err := writer.Close(); err != nil {
		return nil, 0, fmt.Errorf("trek %s %s: %w", http.MethodPost, path, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, &body)
	if err != nil {
		return nil, 0, fmt.Errorf("trek request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("trek %s %s: %w", http.MethodPost, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("trek %s %s response: %w", http.MethodPost, path, err)
	}
	return respBody, resp.StatusCode, nil
}

func tripPath(tripID int64, suffix string) string {
	return tripPathPrefix + "/" + strconv.FormatInt(tripID, 10) + suffix
}

func newAPIError(status int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: status, Message: strings.TrimSpace(string(body))}

	var payload struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		apiErr.Message = payload.Error
		apiErr.Code = payload.Code
	}
	return apiErr
}
