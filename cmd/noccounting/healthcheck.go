package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// healthcheck asks a running instance whether it is ready. The image is
// distroless, with no curl or wget for a Docker HEALTHCHECK to call, so the
// binary checks itself.
func healthcheck(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", url, resp.Status)
	}
	return nil
}
