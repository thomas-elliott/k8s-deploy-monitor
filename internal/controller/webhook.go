package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"time"
)

type WebhookClient struct {
	http *http.Client
}

func NewWebhookClient() *WebhookClient {
	return &WebhookClient{http: &http.Client{Timeout: 10 * time.Second}}
}

func (w *WebhookClient) Post(ctx context.Context, url string, headers http.Header, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	maps.Copy(req.Header, headers)
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("POST %s: %s", url, resp.Status)
	}
	return nil
}
