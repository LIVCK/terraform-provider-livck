// Package client is a minimal, typed HTTP client for the LIVCK public API v1.
//
// Error contract (mirrors the API): 404 → ErrNotFound (Terraform removes the
// resource from state), 422 → *ValidationError with the field error map,
// 401/403 → *APIError. 429 and 5xx are retried transparently with the
// Retry-After header honored (retryablehttp's default backoff).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

// ErrNotFound marks a 404 — the resource is gone (or belongs to another org,
// which the API deliberately does not distinguish).
var ErrNotFound = errors.New("resource not found")

// APIError is a non-validation error response ({"message": ...}).
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("LIVCK API error (HTTP %d): %s", e.StatusCode, e.Message)
}

// ValidationError is a 422 with Laravel's standard {message, errors} envelope.
type ValidationError struct {
	Message string              `json:"message"`
	Errors  map[string][]string `json:"errors"`
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	for field, msgs := range e.Errors {
		fmt.Fprintf(&b, "\n  %s: %s", field, strings.Join(msgs, "; "))
	}
	return b.String()
}

// Client talks to {endpoint}/v1 with a bearer token (lvk_…).
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(endpoint, token string) *Client {
	rc := retryablehttp.NewClient()
	rc.RetryMax = 4
	rc.RetryWaitMin = 1 * time.Second
	rc.RetryWaitMax = 30 * time.Second
	rc.Logger = nil // no default noisy logging inside Terraform

	return &Client{
		baseURL: strings.TrimRight(endpoint, "/") + "/v1",
		token:   token,
		http:    rc.StandardClient(),
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}

	contentType := ""
	if body != nil {
		contentType = "application/json"
	}

	return c.send(req, contentType, out)
}

// doMultipart uploads a single file as multipart/form-data under the given
// field, reusing the same auth, retry and error handling as do(). PHP only
// parses multipart bodies for POST, so callers must use POST.
func (c *Client) doMultipart(ctx context.Context, path, field, filename string, content []byte, out any) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile(field, filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(content); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, &buf)
	if err != nil {
		return err
	}

	return c.send(req, mw.FormDataContentType(), out)
}

// send sets the shared headers, executes the request and maps the response the
// same way for JSON and multipart callers.
func (c *Client) send(req *http.Request, contentType string, out any) error {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	// Deliberately NO Accept-Language: translatable fields must resolve to the
	// org's default locale so reads are stable for state comparison.
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("decoding response: %w", err)
			}
		}
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusUnprocessableEntity:
		ve := &ValidationError{}
		if err := json.Unmarshal(raw, ve); err != nil || ve.Message == "" {
			return &APIError{StatusCode: resp.StatusCode, Message: string(raw)}
		}
		return ve
	default:
		msg := struct {
			Message string `json:"message"`
		}{}
		_ = json.Unmarshal(raw, &msg)
		if msg.Message == "" {
			msg.Message = strings.TrimSpace(string(raw))
		}
		return &APIError{StatusCode: resp.StatusCode, Message: msg.Message}
	}
}

// dataEnvelope unwraps Laravel's {"data": ...} resource envelope.
type dataEnvelope[T any] struct {
	Data T `json:"data"`
}
