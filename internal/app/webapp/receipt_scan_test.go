package webapp

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

func scanServer(t *testing.T, analyzer stubAnalyzer) *httptest.Server {
	t.Helper()
	users := &fakeUserRepo{users: map[int64]*domain.User{
		123456789: {ID: 1, TelegramID: 123456789, BackendUserID: "8", Nickname: "John"},
	}}
	handler, err := NewHandler(user.NewService(users), expense.NewService(&stubAccountingRepo{}, stubRates{}, analyzer), trip.NewService(twoTrips{}), "test-token", true)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	server := httptest.NewUnstartedServer(mux)
	server.Config.ReadTimeout = 300 * time.Millisecond
	server.Config.WriteTimeout = 300 * time.Millisecond
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func photoBody(t *testing.T, size int) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("receipt", "receipt.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("x"), size)); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, form.FormDataContentType()
}

func TestReceiptScanOutlastsTheServerTimeouts(t *testing.T) {
	analysis := &domain.ReceiptAnalysis{Summary: "Matsuya", Total: 650, Currency: domain.CurrencyJPY}
	server := scanServer(t, stubAnalyzer{delay: 700 * time.Millisecond, result: analysis})

	body, contentType := photoBody(t, 64)
	resp, err := http.Post(server.URL+"/api/receipt/analyze", contentType, body)
	if err != nil {
		t.Fatalf("the scan was cut off: %v", err)
	}
	defer resp.Body.Close()

	var answer analyzeReceiptResponse
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !answer.Success || answer.Summary != "Matsuya" {
		t.Errorf("status=%d answer=%+v, want the finished scan", resp.StatusCode, answer)
	}
}

func TestReceiptScanRefusesAPhotoOverTheLimit(t *testing.T) {
	server := scanServer(t, stubAnalyzer{result: &domain.ReceiptAnalysis{}})

	body, contentType := photoBody(t, maxReceiptUpload+1)
	resp, err := http.Post(server.URL+"/api/receipt/analyze", contentType, body)
	if err != nil {
		t.Skipf("the server closed the connection before the answer could be read: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}
}

func TestReceiptScanTellsAnUnsupportedCurrency(t *testing.T) {
	server := scanServer(t, stubAnalyzer{err: domain.ErrUnsupportedCurrency})

	body, contentType := photoBody(t, 64)
	resp, err := http.Post(server.URL+"/api/receipt/analyze", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var answer analyzeReceiptResponse
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(answer.Error, "TWD 或 JPY") {
		t.Errorf("status=%d error=%q, want the currency named as the reason", resp.StatusCode, answer.Error)
	}
}

func TestServerStartFailsWhenThePortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := taken.Addr().(*net.TCPAddr).Port

	users := user.NewService(&fakeUserRepo{users: map[int64]*domain.User{}})
	server, err := NewServer(users, expense.NewService(&stubAccountingRepo{}, nil, nil), newTestTrips(), strconv.Itoa(port), "token", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown(t.Context()) })

	if err := server.Start(); err == nil {
		t.Error("Start() = nil on a port someone else holds, want the failure returned")
	}
}
