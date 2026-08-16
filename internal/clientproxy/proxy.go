// Package clientproxy provides a local Docker API transport that injects a
// short-lived OIDC bearer token into requests made by unmodified clients.
package clientproxy

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const maxTokenBytes = 16 << 10

// Config defines the authenticated upstream transport.
type Config struct {
	Upstream  string
	TokenFile string
	CAFile    string
}

// Proxy injects the current token file contents into each upstream request.
// Reading on every request supports projected service-account token rotation.
type Proxy struct {
	tokenFile string
	upstream  *httputil.ReverseProxy
}

// New constructs a proxy that accepts only an HTTPS upstream.
func New(cfg Config) (*Proxy, error) {
	transport, err := newTransport(strings.TrimSpace(cfg.CAFile))
	if err != nil {
		return nil, err
	}
	return newWithRoundTripper(cfg, transport)
}

func newWithRoundTripper(cfg Config, transport http.RoundTripper) (*Proxy, error) {
	target, err := validateConfig(cfg)
	if err != nil {
		return nil, err
	}
	if transport == nil {
		return nil, errors.New("client proxy transport is required")
	}

	reverseProxy := httputil.NewSingleHostReverseProxy(target)
	director := reverseProxy.Director
	reverseProxy.Director = func(req *http.Request) {
		director(req)
		req.Host = target.Host
	}
	reverseProxy.Transport = transport
	reverseProxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		writeError(w, http.StatusBadGateway, "upstream unavailable")
	}
	return &Proxy{
		tokenFile: strings.TrimSpace(cfg.TokenFile),
		upstream:  reverseProxy,
	}, nil
}

func validateConfig(cfg Config) (*url.URL, error) {
	rawUpstream := strings.TrimRight(strings.TrimSpace(cfg.Upstream), "/")
	if rawUpstream == "" {
		return nil, errors.New("client proxy upstream is required")
	}
	target, err := url.Parse(rawUpstream)
	if err != nil {
		return nil, fmt.Errorf("parse client proxy upstream: %w", err)
	}
	if target.Scheme != "https" || target.Host == "" || target.User != nil ||
		target.RawQuery != "" || target.Fragment != "" {
		return nil, errors.New("client proxy upstream must be an absolute HTTPS URL without credentials, query or fragment")
	}
	if target.Path != "" {
		return nil, errors.New("client proxy upstream must not contain a path")
	}

	tokenFile := strings.TrimSpace(cfg.TokenFile)
	if tokenFile == "" {
		return nil, errors.New("client proxy token file is required")
	}
	if !filepath.IsAbs(tokenFile) {
		return nil, errors.New("client proxy token file must be an absolute path")
	}
	return target, nil
}

func newTransport(caFile string) (*http.Transport, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system CA pool: %w", err)
	}
	if caFile != "" {
		pemData, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read client proxy CA file: %w", err)
		}
		if ok := roots.AppendCertsFromPEM(pemData); !ok {
			return nil, errors.New("client proxy CA file contains no valid certificates")
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}
	return transport, nil
}

// ServeHTTP reads the token immediately before forwarding so a rotated token
// is used without restarting the proxy.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, err := readToken(p.tokenFile)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "bearer token unavailable")
		return
	}
	request := r.Clone(r.Context())
	request.Header = r.Header.Clone()
	request.Header.Set("Authorization", "Bearer "+token)
	p.upstream.ServeHTTP(w, request)
}

func readToken(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, maxTokenBytes+1))
	if err != nil {
		return "", err
	}
	if len(value) > maxTokenBytes {
		return "", errors.New("bearer token is too large")
	}
	token := strings.TrimSpace(string(value))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", errors.New("bearer token file is invalid")
	}
	return token, nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}
