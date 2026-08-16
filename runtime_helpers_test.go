package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRequireUnprivilegedRuntime(t *testing.T) {
	if err := requireUnprivilegedRuntime(1000); err != nil {
		t.Fatalf("requireUnprivilegedRuntime(1000) returned unexpected error: %v", err)
	}
	if err := requireUnprivilegedRuntime(0); err == nil {
		t.Fatalf("requireUnprivilegedRuntime(0) expected error, got nil")
	}
}

func TestParseMemTotal(t *testing.T) {
	data := []byte("MemTotal:       12345 kB\nMemFree:        12 kB\n")
	got := parseMemTotal(data)
	want := int64(12345 * 1024)
	if got != want {
		t.Fatalf("parseMemTotal = %d, want %d", got, want)
	}
}

func TestRequestTimeoutFor(t *testing.T) {
	tests := []struct {
		name   string
		method string
		target string
		want   time.Duration
	}{
		{name: "default", method: http.MethodGet, target: "/version", want: 30 * time.Second},
		{name: "images create", method: http.MethodPost, target: "/images/create?fromImage=redis", want: 10 * time.Minute},
		{name: "logs follow", method: http.MethodGet, target: "/containers/abc/logs?follow=true", want: 0},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.target, nil)
		if got := requestTimeoutFor(req); got != tt.want {
			t.Fatalf("%s: requestTimeoutFor(%s %s) = %s, want %s", tt.name, tt.method, tt.target, got, tt.want)
		}
	}
}

func TestResolveUnixSocketPath(t *testing.T) {
	stateDir := "/tmp/sw"
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: "/tmp/sw/docker.sock"},
		{in: "-", want: ""},
		{in: "off", want: ""},
		{in: "/run/user/1000/sw.sock", want: "/run/user/1000/sw.sock"},
	}
	for _, tt := range tests {
		if got := resolveUnixSocketPath(tt.in, stateDir); got != tt.want {
			t.Fatalf("resolveUnixSocketPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestListenUnixSocketRefusesRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker.sock")
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	if _, err := listenUnixSocket(path); err == nil {
		t.Fatal("listenUnixSocket() should reject a regular file")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	if string(got) != "keep me" {
		t.Fatalf("sentinel content = %q", got)
	}
}

func TestRequestBodyLimitMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
	handler := requestBodyLimitMiddleware(4, 8)(next)

	tooLarge := httptest.NewRequest(http.MethodPost, "/containers/create", strings.NewReader("12345"))
	tooLargeResult := httptest.NewRecorder()
	handler.ServeHTTP(tooLargeResult, tooLarge)
	if tooLargeResult.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("control status = %d, want %d", tooLargeResult.Code, http.StatusRequestEntityTooLarge)
	}

	archive := httptest.NewRequest(http.MethodPut, "/v1.41/containers/abc/archive", strings.NewReader("12345678"))
	archiveResult := httptest.NewRecorder()
	handler.ServeHTTP(archiveResult, archive)
	if archiveResult.Code != http.StatusOK {
		t.Fatalf("archive status = %d, want %d", archiveResult.Code, http.StatusOK)
	}
}

func TestChunkedJSONBodyLimitReturnsEntityTooLarge(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var dst map[string]string
		_ = decodeJSONRequest(w, r, &dst, false)
	})
	handler := requestBodyLimitMiddleware(4, 8)(next)
	req := httptest.NewRequest(http.MethodPost, "/containers/create", strings.NewReader(`{"key":"value"}`))
	req.ContentLength = -1
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, req)
	if result.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", result.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestInsecurePullTransport(t *testing.T) {
	rt := insecurePullTransport()
	tr, ok := rt.(*http.Transport)
	if !ok {
		t.Fatalf("insecurePullTransport() returned %T, want *http.Transport", rt)
	}
	if tr.TLSClientConfig == nil {
		t.Fatalf("TLSClientConfig is nil")
	}
	if !tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("InsecureSkipVerify = false, want true")
	}
}
