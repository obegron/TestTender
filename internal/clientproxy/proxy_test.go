package clientproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestProxyInjectsAndRotatesBearerToken(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("first-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	requests := make(chan *http.Request, 2)
	bodies := make(chan string, 2)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		requests <- r.Clone(r.Context())
		bodies <- string(body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))
	defer upstream.Close()

	proxy, err := newWithRoundTripper(Config{
		Upstream:  upstream.URL,
		TokenFile: tokenFile,
	}, upstream.Client().Transport)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	server := httptest.NewServer(proxy)
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/containers/create?name=db", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer caller-controlled")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("unexpected status: %d", response.StatusCode)
	}
	received := <-requests
	if got := received.Header.Get("Authorization"); got != "Bearer first-token" {
		t.Fatalf("Authorization = %q", got)
	}
	if received.URL.Path != "/containers/create" || received.URL.RawQuery != "name=db" {
		t.Fatalf("unexpected upstream URL: %s", received.URL.String())
	}
	if received.Host != upstream.Listener.Addr().String() {
		t.Fatalf("upstream Host = %q", received.Host)
	}
	if got := <-bodies; got != "" {
		t.Fatalf("unexpected body %q", got)
	}

	if err := os.WriteFile(tokenFile, []byte("second-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	response, err = server.Client().Get(server.URL + "/_ping")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	received = <-requests
	if got := received.Header.Get("Authorization"); got != "Bearer second-token" {
		t.Fatalf("rotated Authorization = %q", got)
	}
	<-bodies
}

func TestProxyFailsClosedWhenTokenUnavailable(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamCalled = true
	}))
	defer upstream.Close()
	proxy, err := newWithRoundTripper(Config{
		Upstream:  upstream.URL,
		TokenFile: filepath.Join(t.TempDir(), "missing"),
	}, upstream.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/_ping", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.Code)
	}
	if upstreamCalled {
		t.Fatal("upstream was called without a token")
	}
}

func TestProxyRejectsUnsafeConfiguration(t *testing.T) {
	for _, cfg := range []Config{
		{},
		{Upstream: "http://testtender.example", TokenFile: "/token"},
		{Upstream: "https://user@testtender.example", TokenFile: "/token"},
		{Upstream: "https://testtender.example/base", TokenFile: "/token"},
		{Upstream: "https://testtender.example", TokenFile: "relative-token"},
	} {
		if _, err := newWithRoundTripper(cfg, http.DefaultTransport); err == nil {
			t.Fatalf("expected configuration rejection: %#v", cfg)
		}
	}
}

func TestReadTokenRejectsEmbeddedNewlineAndOversize(t *testing.T) {
	dir := t.TempDir()
	newline := filepath.Join(dir, "newline")
	if err := os.WriteFile(newline, []byte("one\ntwo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readToken(newline); err == nil {
		t.Fatal("expected embedded newline rejection")
	}
	large := filepath.Join(dir, "large")
	if err := os.WriteFile(large, make([]byte, maxTokenBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readToken(large); err == nil {
		t.Fatal("expected oversized token rejection")
	}
}
