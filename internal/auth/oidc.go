// Package auth authenticates callers of the TestTender API.
package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

const (
	defaultHTTPTimeout     = 10 * time.Second
	defaultRefreshInterval = 15 * time.Minute
	unknownKeyRefreshDelay = 30 * time.Second
	defaultClockSkew       = 30 * time.Second
	maxTokenBytes          = 16 << 10
	maxDiscoveryBytes      = 1 << 20
	maxJWKSBytes           = 2 << 20
)

var validMethods = []string{"RS256", "ES256", "ES384", "ES512"}

// Config defines a single, explicitly trusted OIDC issuer. DiscoveryURL may
// differ from Issuer so an internal helper can publish Kubernetes discovery
// and JWKS documents without changing the token's iss claim.
type Config struct {
	Required          bool
	Issuer            string
	DiscoveryURL      string
	Audience          string
	AllowedSubjects   []string
	AllowedNamespaces []string
	CAFile            string
	HTTPTimeout       time.Duration
	RefreshInterval   time.Duration
	ClockSkew         time.Duration
}

// Identity is the authenticated identity carried by a request.
type Identity struct {
	Issuer                   string
	Subject                  string
	KubernetesNamespace      string
	KubernetesServiceAccount string
	KubernetesPodUID         string
}

type kubernetesClaims struct {
	Namespace      string `json:"namespace"`
	ServiceAccount struct {
		Name string `json:"name"`
		UID  string `json:"uid"`
	} `json:"serviceaccount"`
	Pod struct {
		Name string `json:"name"`
		UID  string `json:"uid"`
	} `json:"pod"`
}

type claims struct {
	jwt.RegisteredClaims
	Kubernetes kubernetesClaims `json:"kubernetes.io"`
}

type keyRecord struct {
	key       any
	algorithm string
}

// Verifier validates signatures and required claims against a cached JWKS.
type Verifier struct {
	cfg               Config
	client            *http.Client
	allowedSubjects   map[string]struct{}
	allowedNamespaces map[string]struct{}

	mu          sync.RWMutex
	keys        map[string]keyRecord
	lastRefresh time.Time
	refreshMu   sync.Mutex
}

type discoveryDocument struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	KeyID     string `json:"kid"`
	KeyType   string `json:"kty"`
	Use       string `json:"use,omitempty"`
	Algorithm string `json:"alg,omitempty"`
	N         string `json:"n,omitempty"`
	E         string `json:"e,omitempty"`
	Curve     string `json:"crv,omitempty"`
	X         string `json:"x,omitempty"`
	Y         string `json:"y,omitempty"`
}

// New creates a verifier and fetches its initial discovery and JWKS documents.
// It returns nil when authentication is entirely unconfigured and not required.
func New(ctx context.Context, cfg Config) (*Verifier, error) {
	normalized, enabled, err := normalizeConfig(cfg)
	if err != nil || !enabled {
		return nil, err
	}

	client, err := newHTTPClient(normalized)
	if err != nil {
		return nil, err
	}
	return newWithHTTPClient(ctx, normalized, client)
}

func newWithHTTPClient(ctx context.Context, cfg Config, client *http.Client) (*Verifier, error) {
	normalized, enabled, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	if client == nil {
		return nil, errors.New("OIDC HTTP client is required")
	}

	v := &Verifier{
		cfg:               normalized,
		client:            client,
		allowedSubjects:   make(map[string]struct{}, len(normalized.AllowedSubjects)),
		allowedNamespaces: make(map[string]struct{}, len(normalized.AllowedNamespaces)),
		keys:              make(map[string]keyRecord),
	}
	for _, subject := range normalized.AllowedSubjects {
		v.allowedSubjects[subject] = struct{}{}
	}
	for _, namespace := range normalized.AllowedNamespaces {
		v.allowedNamespaces[namespace] = struct{}{}
	}
	if err := v.refresh(ctx); err != nil {
		return nil, fmt.Errorf("initialize OIDC keys: %w", err)
	}
	return v, nil
}

func normalizeConfig(cfg Config) (Config, bool, error) {
	cfg.Issuer = strings.TrimSpace(cfg.Issuer)
	cfg.DiscoveryURL = strings.TrimRight(strings.TrimSpace(cfg.DiscoveryURL), "/")
	cfg.Audience = strings.TrimSpace(cfg.Audience)
	cfg.CAFile = strings.TrimSpace(cfg.CAFile)

	configured := cfg.Issuer != "" || cfg.DiscoveryURL != "" || cfg.Audience != "" ||
		len(cfg.AllowedSubjects) != 0 || len(cfg.AllowedNamespaces) != 0 || cfg.CAFile != ""
	if !configured && !cfg.Required {
		return cfg, false, nil
	}
	if cfg.Issuer == "" {
		return cfg, false, errors.New("OIDC issuer is required")
	}
	if cfg.DiscoveryURL == "" {
		cfg.DiscoveryURL = strings.TrimRight(cfg.Issuer, "/")
	}
	if cfg.Audience == "" {
		return cfg, false, errors.New("OIDC audience is required")
	}
	if len(cfg.AllowedSubjects) == 0 && len(cfg.AllowedNamespaces) == 0 {
		return cfg, false, errors.New("at least one exact OIDC subject or Kubernetes namespace is required")
	}

	if err := validateHTTPSURL("OIDC issuer", cfg.Issuer); err != nil {
		return cfg, false, err
	}
	if err := validateHTTPSURL("OIDC discovery URL", cfg.DiscoveryURL); err != nil {
		return cfg, false, err
	}

	subjects, err := normalizeExactValues("OIDC allowed subjects", cfg.AllowedSubjects)
	if err != nil {
		return cfg, false, err
	}
	cfg.AllowedSubjects = subjects

	namespaces, err := normalizeExactValues("OIDC allowed namespaces", cfg.AllowedNamespaces)
	if err != nil {
		return cfg, false, err
	}
	for _, namespace := range namespaces {
		if problems := k8svalidation.IsDNS1123Label(namespace); len(problems) != 0 {
			return cfg, false, fmt.Errorf("invalid OIDC allowed namespace %q: %s", namespace, strings.Join(problems, ", "))
		}
	}
	cfg.AllowedNamespaces = namespaces

	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = defaultHTTPTimeout
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = defaultRefreshInterval
	}
	if cfg.ClockSkew <= 0 {
		cfg.ClockSkew = defaultClockSkew
	}
	return cfg, true, nil
}

func validateHTTPSURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%s must be an absolute HTTPS URL without credentials, query or fragment", name)
	}
	return nil
}

func newHTTPClient(cfg Config) (*http.Client, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system CA pool: %w", err)
	}
	if cfg.CAFile != "" {
		pemData, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read OIDC CA file: %w", err)
		}
		if ok := roots.AppendCertsFromPEM(pemData); !ok {
			return nil, errors.New("OIDC CA file contains no valid certificates")
		}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}
	return &http.Client{
		Timeout:   cfg.HTTPTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many OIDC redirects")
			}
			return validateHTTPSURL("OIDC redirect", req.URL.String())
		},
	}, nil
}

// Verify validates token and returns only the identity fields needed for later
// per-run authorization. It deliberately implements no administrator bypass.
func (v *Verifier) Verify(ctx context.Context, raw string) (Identity, error) {
	if v == nil {
		return Identity{}, errors.New("OIDC verifier is disabled")
	}
	if raw == "" || len(raw) > maxTokenBytes {
		return Identity{}, errors.New("invalid bearer token length")
	}

	parsedClaims := &claims{}
	token, err := jwt.ParseWithClaims(raw, parsedClaims, v.keyForToken(ctx),
		jwt.WithValidMethods(validMethods),
		jwt.WithIssuer(v.cfg.Issuer),
		jwt.WithAudience(v.cfg.Audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(v.cfg.ClockSkew),
		jwt.WithStrictDecoding(),
	)
	if err != nil {
		return Identity{}, fmt.Errorf("validate bearer token: %w", err)
	}
	if !token.Valid {
		return Identity{}, errors.New("bearer token is invalid")
	}
	namespace, serviceAccount, err := kubernetesServiceAccountIdentity(parsedClaims)
	if err != nil {
		return Identity{}, err
	}
	_, subjectAllowed := v.allowedSubjects[parsedClaims.Subject]
	_, namespaceAllowed := v.allowedNamespaces[namespace]
	if !subjectAllowed && !namespaceAllowed {
		return Identity{}, errors.New("bearer token identity is not allowed")
	}

	return Identity{
		Issuer:                   parsedClaims.Issuer,
		Subject:                  parsedClaims.Subject,
		KubernetesNamespace:      namespace,
		KubernetesServiceAccount: serviceAccount,
		KubernetesPodUID:         parsedClaims.Kubernetes.Pod.UID,
	}, nil
}

func normalizeExactValues(name string, values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			return nil, fmt.Errorf("%s cannot contain an empty value", name)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result, nil
}

func kubernetesServiceAccountIdentity(value *claims) (string, string, error) {
	if value.Subject == "" {
		return "", "", errors.New("bearer token subject is required")
	}
	const prefix = "system:serviceaccount:"
	if !strings.HasPrefix(value.Subject, prefix) {
		return "", "", errors.New("bearer token subject is not a Kubernetes service account")
	}
	parts := strings.Split(strings.TrimPrefix(value.Subject, prefix), ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("bearer token service-account subject is malformed")
	}
	if value.Kubernetes.Namespace != parts[0] || value.Kubernetes.ServiceAccount.Name != parts[1] {
		return "", "", errors.New("bearer token Kubernetes claims do not match its subject")
	}
	return parts[0], parts[1], nil
}

func (v *Verifier) keyForToken(ctx context.Context) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || strings.TrimSpace(kid) == "" {
			return nil, errors.New("bearer token has no key ID")
		}
		kid = strings.TrimSpace(kid)

		v.mu.RLock()
		record, found := v.keys[kid]
		lastRefresh := v.lastRefresh
		v.mu.RUnlock()

		if found && time.Since(lastRefresh) < v.cfg.RefreshInterval {
			return checkKeyAlgorithm(record, token.Method.Alg())
		}
		if err := v.refreshIfAllowed(ctx, !found); err != nil {
			return nil, err
		}

		v.mu.RLock()
		record, found = v.keys[kid]
		v.mu.RUnlock()
		if !found {
			return nil, fmt.Errorf("OIDC key ID %q is not trusted", kid)
		}
		return checkKeyAlgorithm(record, token.Method.Alg())
	}
}

func checkKeyAlgorithm(record keyRecord, tokenAlgorithm string) (any, error) {
	if record.algorithm != "" && record.algorithm != tokenAlgorithm {
		return nil, fmt.Errorf("OIDC key algorithm %q does not match token algorithm %q", record.algorithm, tokenAlgorithm)
	}
	switch key := record.key.(type) {
	case *rsa.PublicKey:
		if tokenAlgorithm != "RS256" {
			return nil, fmt.Errorf("RSA key cannot verify %s", tokenAlgorithm)
		}
		return key, nil
	case *ecdsa.PublicKey:
		expected := map[string]string{
			"P-256": "ES256",
			"P-384": "ES384",
			"P-521": "ES512",
		}[key.Curve.Params().Name]
		if expected == "" || expected != tokenAlgorithm {
			return nil, fmt.Errorf("EC key cannot verify %s", tokenAlgorithm)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported OIDC key type %T", record.key)
	}
}

func (v *Verifier) refreshIfAllowed(ctx context.Context, unknownKey bool) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()

	v.mu.RLock()
	lastRefresh := v.lastRefresh
	v.mu.RUnlock()
	delay := v.cfg.RefreshInterval
	if unknownKey {
		delay = unknownKeyRefreshDelay
	}
	if !lastRefresh.IsZero() && time.Since(lastRefresh) < delay {
		return nil
	}
	return v.refreshLocked(ctx)
}

func (v *Verifier) refresh(ctx context.Context) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()
	return v.refreshLocked(ctx)
}

func (v *Verifier) refreshLocked(ctx context.Context) error {
	discoveryURL := v.cfg.DiscoveryURL + "/.well-known/openid-configuration"
	var discovery discoveryDocument
	if err := v.fetchJSON(ctx, discoveryURL, maxDiscoveryBytes, &discovery); err != nil {
		return fmt.Errorf("fetch OIDC discovery: %w", err)
	}
	if discovery.Issuer != v.cfg.Issuer {
		return fmt.Errorf("OIDC discovery issuer %q does not match configured issuer", discovery.Issuer)
	}
	if err := validateHTTPSURL("OIDC JWKS URI", discovery.JWKSURI); err != nil {
		return err
	}

	var document jwksDocument
	if err := v.fetchJSON(ctx, discovery.JWKSURI, maxJWKSBytes, &document); err != nil {
		return fmt.Errorf("fetch OIDC JWKS: %w", err)
	}
	keys, err := parseKeySet(document)
	if err != nil {
		return err
	}

	v.mu.Lock()
	v.keys = keys
	v.lastRefresh = time.Now()
	v.mu.Unlock()
	return nil
}

func (v *Verifier) fetchJSON(ctx context.Context, endpoint string, limit int64, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("endpoint returned HTTP %d", resp.StatusCode)
	}

	decoder := json.NewDecoder(io.LimitReader(resp.Body, limit+1))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("endpoint returned multiple JSON values")
		}
		return err
	}
	return nil
}

func parseKeySet(document jwksDocument) (map[string]keyRecord, error) {
	if len(document.Keys) == 0 {
		return nil, errors.New("OIDC JWKS contains no keys")
	}
	keys := make(map[string]keyRecord, len(document.Keys))
	for _, value := range document.Keys {
		if value.Use != "" && value.Use != "sig" {
			continue
		}
		if value.KeyID == "" {
			return nil, errors.New("OIDC signing key has no key ID")
		}
		if _, duplicate := keys[value.KeyID]; duplicate {
			return nil, fmt.Errorf("OIDC JWKS contains duplicate key ID %q", value.KeyID)
		}

		var (
			key any
			err error
		)
		switch value.KeyType {
		case "RSA":
			key, err = parseRSAPublicKey(value.N, value.E)
		case "EC":
			key, err = parseECPublicKey(value.Curve, value.X, value.Y)
		default:
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("parse OIDC key %q: %w", value.KeyID, err)
		}
		if value.Algorithm != "" && !slices.Contains(validMethods, value.Algorithm) {
			continue
		}
		keys[value.KeyID] = keyRecord{key: key, algorithm: value.Algorithm}
	}
	if len(keys) == 0 {
		return nil, errors.New("OIDC JWKS contains no supported signing keys")
	}
	return keys, nil
}

func parseRSAPublicKey(encodedN, encodedE string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(encodedN)
	if err != nil || len(nBytes) == 0 {
		return nil, errors.New("invalid RSA modulus")
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(encodedE)
	if err != nil || len(eBytes) == 0 {
		return nil, errors.New("invalid RSA exponent")
	}
	n := new(big.Int).SetBytes(nBytes)
	if n.BitLen() < 2048 {
		return nil, errors.New("RSA modulus must be at least 2048 bits")
	}
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() {
		return nil, errors.New("RSA exponent is too large")
	}
	exponent := e.Int64()
	if exponent < 3 || exponent%2 == 0 || int64(int(exponent)) != exponent {
		return nil, errors.New("invalid RSA exponent")
	}
	return &rsa.PublicKey{N: n, E: int(exponent)}, nil
}

func parseECPublicKey(curveName, encodedX, encodedY string) (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve
	switch curveName {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported EC curve %q", curveName)
	}
	xBytes, err := base64.RawURLEncoding.DecodeString(encodedX)
	if err != nil || len(xBytes) == 0 {
		return nil, errors.New("invalid EC x coordinate")
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(encodedY)
	if err != nil || len(yBytes) == 0 {
		return nil, errors.New("invalid EC y coordinate")
	}
	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)
	if !curve.IsOnCurve(x, y) {
		return nil, errors.New("EC point is not on the configured curve")
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}
