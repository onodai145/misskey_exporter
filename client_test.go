package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAPIKey = "secret-token-123"

func TestClientPostSuccess(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		io.WriteString(w, `{"deliver":{"waiting":2}}`)
	}))
	defer srv.Close()

	var out map[string]map[string]float64
	c := newClient(srv.URL, testAPIKey)
	if err := c.Post(context.Background(), "/api/admin/queue/stats", &out); err != nil {
		t.Fatalf("Post: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/admin/queue/stats" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer "+testAPIKey {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q", gotContentType)
	}
	if gotBody != "{}" {
		t.Errorf("body = %q, want {}", gotBody)
	}
	if out["deliver"]["waiting"] != 2 {
		t.Errorf("decoded = %v", out)
	}
}

// Misskey behind a reverse proxy under a sub path (see config normalization).
func TestClientPostWithPathPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	var out map[string]any
	c := newClient(srv.URL+"/misskey", testAPIKey)
	if err := c.Post(context.Background(), "/api/x", &out); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotPath != "/misskey/api/x" {
		t.Errorf("path = %q, want /misskey/api/x", gotPath)
	}
}

// The token lacking a permission (see README) makes Misskey answer 4xx.
func TestClientPostNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"message":"Insufficient permissions."}}`)
	}))
	defer srv.Close()

	var out map[string]any
	err := newClient(srv.URL, testAPIKey).Post(context.Background(), "/api/x", &out)
	if err == nil {
		t.Fatal("Post succeeded, want error")
	}
	for _, want := range []string{"403", "Insufficient permissions."} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), testAPIKey) {
		t.Errorf("error %q leaks the API key", err)
	}
}

func TestClientPostTruncatesErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, strings.Repeat("z", 5000))
	}))
	defer srv.Close()

	var out map[string]any
	err := newClient(srv.URL, testAPIKey).Post(context.Background(), "/api/x", &out)
	if err == nil {
		t.Fatal("Post succeeded, want error")
	}
	if n := strings.Count(err.Error(), "z"); n != maxErrorBodyBytes {
		t.Errorf("error contains %d body bytes, want %d", n, maxErrorBodyBytes)
	}
}

func TestClientPostInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<html>not json</html>`)
	}))
	defer srv.Close()

	var out map[string]any
	err := newClient(srv.URL, testAPIKey).Post(context.Background(), "/api/x", &out)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("err = %v, want a decode error", err)
	}
}

func TestClientPostResponseTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"a":"`+strings.Repeat("a", 100)+`"}`)
	}))
	defer srv.Close()

	c := newClient(srv.URL, testAPIKey)
	c.maxResponseBytes = 16
	var out map[string]any
	err := c.Post(context.Background(), "/api/x", &out)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want a size limit error", err)
	}
}

// A hung Misskey must not hang the scrape: the caller's context decides.
func TestClientPostContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server only notices a closed connection after the body is read.
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	var out map[string]any
	err := newClient(srv.URL, testAPIKey).Post(ctx, "/api/x", &out)
	if err == nil {
		t.Fatal("Post succeeded, want error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Post took %v, want it to stop at the context deadline", elapsed)
	}
}

// Transport errors embed the request URL; the token must never be part of it.
func TestClientPostConnectionErrorDoesNotLeakKey(t *testing.T) {
	var out map[string]any
	err := newClient("http://127.0.0.1:1", testAPIKey).Post(context.Background(), "/api/x", &out)
	if err == nil {
		t.Fatal("Post succeeded, want error")
	}
	if strings.Contains(err.Error(), testAPIKey) {
		t.Errorf("error %q leaks the API key", err)
	}
}
