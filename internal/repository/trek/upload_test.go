package trek_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/repository/trek"
)

const (
	filesPath = "/api/trips/3/files"

	uploadField = "file"

	uploadMemory = 8 << 20
)

var (
	jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}, []byte("noccounting receipt")...)
	pngBytes  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("noccounting receipt")...)
)

type uploadSeen struct {
	path        string
	contentType string // the request's own Content-Type, boundary included
	auth        string
	files       []string // every form field that carried a file
	values      []string // every form field that carried a value
	count       int      // how many parts arrived under uploadField
	filename    string
	partType    string
	content     []byte
	err         error
}

func (u uploadSeen) String() string {
	return fmt.Sprintf("%s %s: files=%v values=%v filename=%q type=%q bytes=%d err=%v",
		u.auth, u.path, u.files, u.values, u.filename, u.partType, len(u.content), u.err)
}

func isFilesCollection(path string) bool {
	rest, isTrip := strings.CutPrefix(path, "/api/trips/")
	if !isTrip {
		return false
	}
	trip, isFiles := strings.CutSuffix(rest, "/files")
	return isFiles && trip != ""
}

func (s *createStub) handleUpload(w http.ResponseWriter, r *http.Request) {
	seen := readUpload(r)

	s.mu.Lock()
	s.calls = append(s.calls, createCall{method: r.Method, path: r.URL.Path})
	s.uploads = append(s.uploads, seen)
	call := len(s.uploads)
	answer, overridden := s.fileOnCall[call]
	status, body := s.fileStatus, s.fileBody
	s.mu.Unlock()

	switch {
	case seen.err != nil:
		writeJSON(w, http.StatusBadRequest, `{"error":"the stub could not read that upload"}`)
	case overridden:
		writeJSON(w, answer.status, answer.body)
	default:
		writeJSON(w, status, body)
	}
}

func readUpload(r *http.Request) uploadSeen {
	seen := uploadSeen{path: r.URL.Path, contentType: r.Header.Get("Content-Type"), auth: r.Header.Get("Authorization")}

	if err := r.ParseMultipartForm(uploadMemory); err != nil {
		seen.err = err
		return seen
	}
	for name := range r.MultipartForm.File {
		seen.files = append(seen.files, name)
	}
	for name := range r.MultipartForm.Value {
		seen.values = append(seen.values, name)
	}
	slices.Sort(seen.files)
	slices.Sort(seen.values)

	parts := r.MultipartForm.File[uploadField]
	seen.count = len(parts)
	if len(parts) == 0 {
		seen.err = fmt.Errorf("the form carried no file under %q", uploadField)
		return seen
	}

	part := parts[0]
	opened, err := part.Open()
	if err != nil {
		seen.err = err
		return seen
	}
	defer opened.Close()

	content, err := io.ReadAll(opened)
	seen.err = err
	seen.filename, seen.partType, seen.content = part.Filename, part.Header.Get("Content-Type"), content
	return seen
}

func (s *createStub) uploadsSeen() []uploadSeen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uploadSeen(nil), s.uploads...)
}

func (s *createStub) oneUpload(t *testing.T) uploadSeen {
	t.Helper()

	uploads := s.uploadsSeen()
	if len(uploads) != 1 {
		t.Fatalf("uploads = %d, want exactly 1 (%v)", len(uploads), s.paths())
	}
	return uploads[0]
}

func receiptFile(t *testing.T, name string, content []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
	return path
}

func TestUploadFile_SendsTheReceiptAsAMultipartBodyUnderTheFieldNameTREKReads(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	path := receiptFile(t, "receipt-4242.jpg", jpegBytes)

	fileID, err := repo.UploadFile(context.Background(), path)
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if fileID != "77" {
		t.Errorf("UploadFile() = %q, want %q — the file id is what receipt_file_ids links", fileID, "77")
	}

	upload := stub.oneUpload(t)
	if upload.err != nil {
		t.Fatalf("the stub could not read the upload: %v", upload)
	}
	if upload.path != filesPath {
		t.Errorf("the receipt went to %q, want %q — the trip binding is the only trip it may reach", upload.path, filesPath)
	}
	if !strings.HasPrefix(upload.contentType, "multipart/form-data; boundary=") {
		t.Errorf("Content-Type = %q, want a multipart/form-data body with a boundary", upload.contentType)
	}
	if want := []string{uploadField}; !slices.Equal(upload.files, want) {
		t.Errorf("the form carried files under %v, want exactly %v", upload.files, want)
	}
	if upload.count != 1 {
		t.Errorf("the form carried %d parts under %q, want 1", upload.count, uploadField)
	}
	if len(upload.values) != 0 {
		t.Errorf("the form carried the value fields %v, want none", upload.values)
	}
	if upload.filename != "receipt-4242.jpg" {
		t.Errorf("the part is named %q, want the base name of the file, which is what TREK stores as original_name", upload.filename)
	}
	if !bytes.Equal(upload.content, jpegBytes) {
		t.Errorf("the part carries %d bytes %x, want the %d bytes on disk", len(upload.content), upload.content, len(jpegBytes))
	}
	if upload.partType != "image/jpeg" {
		t.Errorf("the part declares %q, want the type sniffed from the bytes", upload.partType)
	}
	if upload.auth != "Bearer token-1" {
		t.Errorf("the upload was authorized with %q, want the session token", upload.auth)
	}
}

func TestUploadFile_SniffsTheContentTypeOfTheBytesItSends(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content []byte
		want    string
	}{
		{name: "a JPEG receipt", file: "receipt-4242.jpg", content: jpegBytes, want: "image/jpeg"},
		{name: "a PNG receipt", file: "receipt-4242.png", content: pngBytes, want: "image/png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			if _, err := repo.UploadFile(context.Background(), receiptFile(t, tt.file, tt.content)); err != nil {
				t.Fatalf("UploadFile() error = %v", err)
			}

			upload := stub.oneUpload(t)
			if upload.partType != tt.want {
				t.Errorf("the part declares %q, want %q", upload.partType, tt.want)
			}
			if upload.filename != tt.file {
				t.Errorf("the part is named %q, want %q", upload.filename, tt.file)
			}
		})
	}
}

func TestUploadFile_EscapesAFileNameThatWouldBreakTheHeader(t *testing.T) {
	stub := newCreateStub(t)
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	path := receiptFile(t, `receipt"42; name=other.jpg`, jpegBytes)

	if _, err := repo.UploadFile(context.Background(), path); err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}

	upload := stub.oneUpload(t)
	if upload.err != nil {
		t.Fatalf("the stub could not read the upload: %v", upload)
	}
	if want := `receipt"42; name=other.jpg`; upload.filename != want {
		t.Errorf("the part is named %q, want %q — the name has to survive the header", upload.filename, want)
	}
	if !slices.Equal(upload.files, []string{uploadField}) {
		t.Errorf("the form carried files under %v, want exactly %v", upload.files, []string{uploadField})
	}
}

func TestUploadFile_ReplaysTheWholeBodyAfterARejectedToken(t *testing.T) {
	stub := newCreateStub(t)
	stub.fileOnCall[1] = stubAnswer{
		status: http.StatusUnauthorized,
		body:   `{"error":"Access token required","code":"AUTH_REQUIRED"}`,
	}
	repo := stub.repo(t, domain.CurrencyTWD, nil)
	path := receiptFile(t, "receipt-4242.jpg", jpegBytes)

	fileID, err := repo.UploadFile(context.Background(), path)
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if fileID != "77" {
		t.Errorf("UploadFile() = %q, want %q — the replay's answer, not the first one's", fileID, "77")
	}

	uploads := stub.uploadsSeen()
	if len(uploads) != 2 {
		t.Fatalf("uploads = %d, want the original and one replay (%v)", len(uploads), stub.paths())
	}
	for i, upload := range uploads {
		if upload.err != nil {
			t.Fatalf("upload %d: the stub could not read it: %v", i+1, upload.err)
		}
		if !slices.Equal(upload.files, []string{uploadField}) || upload.count != 1 {
			t.Errorf("upload %d carried %d part(s) under %v, want the one part under %q", i+1, upload.count, upload.files, uploadField)
		}
		if upload.filename != "receipt-4242.jpg" {
			t.Errorf("upload %d is named %q, want the file's own name", i+1, upload.filename)
		}
		if !bytes.Equal(upload.content, jpegBytes) {
			t.Errorf("upload %d carries %d bytes, want the %d on disk — the replay has to rebuild the body", i+1, len(upload.content), len(jpegBytes))
		}
	}
}

func TestUploadFile_ReportsAFileItCannotReadWithoutCallingTREK(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "no file at all", path: filepath.Join(dir, "receipt-gone.jpg")},
		{name: "a directory", path: dir},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stub := newCreateStub(t)
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			fileID, err := repo.UploadFile(context.Background(), tt.path)
			if err == nil {
				t.Fatalf("UploadFile() = %q, want the file it could not read", fileID)
			}
			if fileID != "" {
				t.Errorf("UploadFile() = %q, want no id when no file was sent", fileID)
			}
			if !strings.Contains(err.Error(), filepath.Base(tt.path)) {
				t.Errorf("error %q does not name the file it could not read", err)
			}
			if uploads := stub.uploadsSeen(); len(uploads) != 0 {
				t.Errorf("a file that was never read was uploaded %d time(s)", len(uploads))
			}
		})
	}
}

func TestUploadFile_ReportsATripThatRefusesTheUpload(t *testing.T) {
	stub := newCreateStub(t)
	stub.fileStatus = http.StatusForbidden
	stub.fileBody = `{"error":"No permission to upload files"}`
	repo := stub.repo(t, domain.CurrencyTWD, nil)

	_, err := repo.UploadFile(context.Background(), receiptFile(t, "receipt-4242.jpg", jpegBytes))
	if err == nil {
		t.Fatal("UploadFile() = nil, want TREK's refusal")
	}
	var apiErr *trek.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("UploadFile() error = %v, want an *trek.APIError", err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want 403", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Message, "No permission to upload files") {
		t.Errorf("Message = %q, want TREK's own text", apiErr.Message)
	}
}

func TestUploadFile_ReportsAnAnswerWithNoFileToLink(t *testing.T) {
	for _, reply := range []string{
		`{}`,
		`{"file":{"filename":"3f9c1a.jpg"}}`,
		`{"file":{"id":0}}`,
		`<html>maintenance</html>`,
	} {
		t.Run(reply, func(t *testing.T) {
			stub := newCreateStub(t)
			stub.fileBody = reply
			repo := stub.repo(t, domain.CurrencyTWD, nil)

			fileID, err := repo.UploadFile(context.Background(), receiptFile(t, "receipt-4242.jpg", jpegBytes))
			if err == nil {
				t.Fatalf("UploadFile() = %q for %s, want a refusal rather than an id that links nothing", fileID, reply)
			}
			if fileID != "" {
				t.Errorf("UploadFile() = %q, want no id", fileID)
			}
		})
	}
}

func TestUploadFile_UsesTheTripTheRepoIsBoundTo(t *testing.T) {
	stub := newCreateStub(t)
	repo := &tripRepoFixture{repo: trek.NewRepo(trek.NewClientWithBaseURL(createConfig(), stub.server.URL), nil), trip: domain.Trip{ID: 9, Currency: domain.CurrencyTWD}}

	if _, err := repo.UploadFile(context.Background(), receiptFile(t, "receipt-4242.jpg", jpegBytes)); err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}

	if got, want := stub.oneUpload(t).path, "/api/trips/9/files"; got != want {
		t.Errorf("the receipt went to %q, want %q — the trip the client is bound to", got, want)
	}
}
