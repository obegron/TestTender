package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testAudience = "testtender"
	testSubject  = "system:serviceaccount:tenant-dev:integration-runner"
)

type oidcFixture struct {
	t         *testing.T
	server    *httptest.Server
	private   *rsa.PrivateKey
	kid       string
	mu        sync.RWMutex
	keys      []jwk
	discovery discoveryDocument
}

func newOIDCFixture(t *testing.T) *oidcFixture {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &oidcFixture{t: t, private: privateKey, kid: "key-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.RLock()
		defer f.mu.RUnlock()
		writeTestJSON(t, w, f.discovery)
	})
	mux.HandleFunc("/openid/v1/jwks", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.RLock()
		defer f.mu.RUnlock()
		writeTestJSON(t, w, jwksDocument{Keys: f.keys})
	})
	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	f.discovery = discoveryDocument{
		Issuer:  f.server.URL,
		JWKSURI: f.server.URL + "/openid/v1/jwks",
	}
	f.keys = []jwk{rsaJWK(f.kid, &privateKey.PublicKey)}
	return f
}

func (f *oidcFixture) verifier(t *testing.T) *Verifier {
	t.Helper()
	verifier, err := newWithHTTPClient(context.Background(), Config{
		Required:        true,
		Issuer:          f.server.URL,
		DiscoveryURL:    f.server.URL,
		Audience:        testAudience,
		AllowedSubjects: []string{testSubject},
		ClockSkew:       time.Nanosecond,
	}, f.server.Client())
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	return verifier
}

func (f *oidcFixture) namespaceVerifier(t *testing.T) *Verifier {
	t.Helper()
	verifier, err := newWithHTTPClient(context.Background(), Config{
		Required:          true,
		Issuer:            f.server.URL,
		DiscoveryURL:      f.server.URL,
		Audience:          testAudience,
		AllowedNamespaces: []string{"tenant-dev"},
		ClockSkew:         time.Nanosecond,
	}, f.server.Client())
	if err != nil {
		t.Fatalf("create namespace verifier: %v", err)
	}
	return verifier
}

func (f *oidcFixture) token(t *testing.T, mutate func(jwt.MapClaims)) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": f.server.URL,
		"sub": testSubject,
		"aud": []string{testAudience},
		"exp": time.Now().Add(5 * time.Minute).Unix(),
		"nbf": time.Now().Add(-time.Minute).Unix(),
		"kubernetes.io": map[string]any{
			"namespace": "tenant-dev",
			"serviceaccount": map[string]any{
				"name": "integration-runner",
				"uid":  "sa-uid",
			},
			"pod": map[string]any{
				"name": "runner-abc",
				"uid":  "pod-uid",
			},
		},
	}
	if mutate != nil {
		mutate(claims)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = f.kid
	raw, err := token.SignedString(f.private)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestVerifierAcceptsExactKubernetesIdentity(t *testing.T) {
	f := newOIDCFixture(t)
	identity, err := f.verifier(t).Verify(context.Background(), f.token(t, nil))
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if identity.Issuer != f.server.URL || identity.Subject != testSubject {
		t.Fatalf("unexpected identity: %#v", identity)
	}
	if identity.KubernetesNamespace != "tenant-dev" ||
		identity.KubernetesServiceAccount != "integration-runner" ||
		identity.KubernetesPodUID != "pod-uid" {
		t.Fatalf("unexpected Kubernetes identity: %#v", identity)
	}
}

func TestVerifierAcceptsExactCICDNamespace(t *testing.T) {
	f := newOIDCFixture(t)
	identity, err := f.namespaceVerifier(t).Verify(context.Background(), f.token(t, nil))
	if err != nil {
		t.Fatalf("verify namespace-authorized token: %v", err)
	}
	if identity.KubernetesNamespace != "tenant-dev" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
}

func TestVerifierRejectsInconsistentOrUnlistedCICDNamespace(t *testing.T) {
	f := newOIDCFixture(t)
	verifier := f.namespaceVerifier(t)

	inconsistent := f.token(t, func(c jwt.MapClaims) {
		c["sub"] = "system:serviceaccount:other-dev:integration-runner"
	})
	if _, err := verifier.Verify(context.Background(), inconsistent); err == nil {
		t.Fatal("expected inconsistent Kubernetes namespace rejection")
	}

	unlisted := f.token(t, func(c jwt.MapClaims) {
		c["sub"] = "system:serviceaccount:other-dev:integration-runner"
		kubernetes := c["kubernetes.io"].(map[string]any)
		kubernetes["namespace"] = "other-dev"
	})
	if _, err := verifier.Verify(context.Background(), unlisted); err == nil {
		t.Fatal("expected unlisted Kubernetes namespace rejection")
	}
}

func TestVerifierRejectsInvalidClaims(t *testing.T) {
	f := newOIDCFixture(t)
	verifier := f.verifier(t)
	tests := []struct {
		name   string
		mutate func(jwt.MapClaims)
	}{
		{name: "issuer", mutate: func(c jwt.MapClaims) { c["iss"] = "https://untrusted.example" }},
		{name: "audience", mutate: func(c jwt.MapClaims) { c["aud"] = []string{"another-service"} }},
		{name: "subject", mutate: func(c jwt.MapClaims) { c["sub"] = "system:serviceaccount:other:runner" }},
		{name: "expiration missing", mutate: func(c jwt.MapClaims) { delete(c, "exp") }},
		{name: "expired", mutate: func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{name: "not active", mutate: func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Minute).Unix() }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := verifier.Verify(context.Background(), f.token(t, test.mutate)); err == nil {
				t.Fatal("expected token rejection")
			}
		})
	}
}

func TestVerifierRejectsWrongSignatureAlgorithmAndKeyID(t *testing.T) {
	f := newOIDCFixture(t)
	verifier := f.verifier(t)

	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": f.server.URL, "sub": testSubject, "aud": testAudience,
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	token.Header["kid"] = f.kid
	raw, err := token.SignedString(otherKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(context.Background(), raw); err == nil {
		t.Fatal("expected wrong signature rejection")
	}

	token = jwt.NewWithClaims(jwt.SigningMethodHS256, token.Claims)
	token.Header["kid"] = f.kid
	raw, err = token.SignedString([]byte("not-a-public-key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(context.Background(), raw); err == nil {
		t.Fatal("expected algorithm rejection")
	}

	raw = f.token(t, nil)
	parts := strings.Split(raw, ".")
	var header map[string]any
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatal(err)
	}
	delete(header, "kid")
	headerJSON, err = json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	parts[0] = base64.RawURLEncoding.EncodeToString(headerJSON)
	if _, err := verifier.Verify(context.Background(), strings.Join(parts, ".")); err == nil {
		t.Fatal("expected missing key ID rejection")
	}
}

func TestVerifierRefreshesForRotatedKey(t *testing.T) {
	f := newOIDCFixture(t)
	verifier := f.verifier(t)
	secondKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.private = secondKey
	f.kid = "key-2"
	f.keys = []jwk{rsaJWK(f.kid, &secondKey.PublicKey)}
	f.mu.Unlock()
	verifier.mu.Lock()
	verifier.lastRefresh = time.Now().Add(-unknownKeyRefreshDelay - time.Second)
	verifier.mu.Unlock()

	if _, err := verifier.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("verify token after key rotation: %v", err)
	}
}

func TestVerifierRejectsDiscoveryIssuerMismatch(t *testing.T) {
	f := newOIDCFixture(t)
	f.mu.Lock()
	f.discovery.Issuer = "https://different-issuer.example"
	f.mu.Unlock()
	_, err := newWithHTTPClient(context.Background(), Config{
		Required:        true,
		Issuer:          f.server.URL,
		DiscoveryURL:    f.server.URL,
		Audience:        testAudience,
		AllowedSubjects: []string{testSubject},
	}, f.server.Client())
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected discovery issuer mismatch, got %v", err)
	}
}

func TestNormalizeConfigIsFailClosed(t *testing.T) {
	if _, enabled, err := normalizeConfig(Config{}); err != nil || enabled {
		t.Fatalf("empty optional config should be disabled, enabled=%v err=%v", enabled, err)
	}
	tests := []Config{
		{Required: true},
		{Issuer: "https://issuer.example", Audience: testAudience},
		{Issuer: "http://issuer.example", Audience: testAudience, AllowedSubjects: []string{testSubject}},
		{Issuer: "https://issuer.example", Audience: testAudience, AllowedNamespaces: []string{"Not_A_Namespace"}},
	}
	for _, cfg := range tests {
		if _, _, err := normalizeConfig(cfg); err == nil {
			t.Fatalf("expected invalid configuration to fail: %#v", cfg)
		}
	}
}

func TestParseKeySetRejectsWeakOrAmbiguousKeys(t *testing.T) {
	weakKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseKeySet(jwksDocument{Keys: []jwk{rsaJWK("weak", &weakKey.PublicKey)}}); err == nil {
		t.Fatal("expected weak RSA key rejection")
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	key := rsaJWK("duplicate", &privateKey.PublicKey)
	if _, err := parseKeySet(jwksDocument{Keys: []jwk{key, key}}); err == nil {
		t.Fatal("expected duplicate key ID rejection")
	}
}

func TestParseECPublicKey(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encodedX := base64.RawURLEncoding.EncodeToString(privateKey.X.Bytes())
	encodedY := base64.RawURLEncoding.EncodeToString(privateKey.Y.Bytes())
	parsed, err := parseECPublicKey("P-256", encodedX, encodedY)
	if err != nil {
		t.Fatalf("parse EC key: %v", err)
	}
	if parsed.X.Cmp(privateKey.X) != 0 || parsed.Y.Cmp(privateKey.Y) != 0 {
		t.Fatal("parsed EC key does not match")
	}
	if _, err := parseECPublicKey("P-256", encodedX, encodedX); err == nil {
		t.Fatal("expected off-curve point rejection")
	}
}

func rsaJWK(kid string, key *rsa.PublicKey) jwk {
	return jwk{
		KeyID:     kid,
		KeyType:   "RSA",
		Use:       "sig",
		Algorithm: "RS256",
		N:         base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		E:         base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode test response: %v", err)
	}
}
