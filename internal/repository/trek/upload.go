package trek

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

const (
	filesPathSuffix = "/files"

	uploadFieldName = "file"

	sniffLen = 512
)

type fileUploadResponse struct {
	File struct {
		ID int64 `json:"id"`
	} `json:"file"`
}

func (r *tripRepo) UploadFile(ctx context.Context, filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("opening the receipt %q to upload it: %w", filePath, err)
	}
	defer file.Close()

	name := filepath.Base(filePath)

	contentType, err := sniffContentType(file)
	if err != nil {
		return "", fmt.Errorf("reading the receipt %q: %w", filePath, err)
	}

	var uploaded fileUploadResponse
	path := tripPath(r.tripID, filesPathSuffix)
	if err := r.client.postMultipart(ctx, path, uploadFieldName, name, file, contentType, &uploaded); err != nil {
		return "", fmt.Errorf("uploading the receipt to TREK trip %d: %w", r.tripID, err)
	}

	if uploaded.File.ID <= 0 {
		return "", fmt.Errorf("TREK answered the receipt upload to trip %d with no file to link", r.tripID)
	}

	slog.Debug("uploaded a receipt to TREK",
		"trip_id", r.tripID, "file_id", uploaded.File.ID, "filename", name, "mime_type", contentType)
	return strconv.FormatInt(uploaded.File.ID, 10), nil
}

func sniffContentType(file *os.File) (string, error) {
	head := make([]byte, sniffLen)
	read, err := file.Read(head)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading the head of the file: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("putting the head of the file back: %w", err)
	}
	return http.DetectContentType(head[:read]), nil
}
