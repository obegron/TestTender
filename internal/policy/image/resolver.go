// Package image implements fail-closed image authorization and mirror
// rewriting for container requests.
package image

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/distribution/reference"
)

const policyVersion = "v1"

// Resolver authorizes a requested image and returns the image Kubernetes
// should run. Implementations must be safe for concurrent use.
type Resolver interface {
	Resolve(requested string) (string, error)
	Enabled() bool
}

// Policy is the on-disk image policy format.
type Policy struct {
	Version       string `json:"version"`
	DefaultAction string `json:"defaultAction"`
	Rules         []Rule `json:"rules"`
}

// Rule authorizes Source and optionally rewrites it to Target. Source and
// Target are normalized OCI/Docker image references when the policy loads.
type Rule struct {
	Source string `json:"source"`
	Target string `json:"target,omitempty"`
}

// DeniedError reports an image rejected by policy.
type DeniedError struct {
	Requested string
}

func (e *DeniedError) Error() string {
	return fmt.Sprintf("image %q is not allowed by policy", e.Requested)
}

type resolver struct {
	enabled        bool
	allowUnmatched bool
	rules          map[string]string
}

// Passthrough returns a resolver that preserves upstream behavior. It is used
// only when no policy file is configured.
func Passthrough() Resolver {
	return &resolver{}
}

// LoadFile loads and validates an image policy. An empty path explicitly
// selects passthrough behavior; a configured but unreadable or invalid policy
// is always an error so startup fails closed.
func LoadFile(path string) (Resolver, error) {
	if strings.TrimSpace(path) == "" {
		return Passthrough(), nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open image policy %q: %w", path, err)
	}
	defer f.Close()

	return Decode(f)
}

// Decode validates a JSON image policy and constructs an immutable resolver.
func Decode(r io.Reader) (Resolver, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var policy Policy
	if err := dec.Decode(&policy); err != nil {
		return nil, fmt.Errorf("decode image policy: %w", err)
	}
	if err := ensureEOF(dec); err != nil {
		return nil, err
	}
	if policy.Version != policyVersion {
		return nil, fmt.Errorf("unsupported image policy version %q (expected %q)", policy.Version, policyVersion)
	}

	allowUnmatched := false
	switch strings.ToLower(strings.TrimSpace(policy.DefaultAction)) {
	case "allow":
		allowUnmatched = true
	case "deny":
	default:
		return nil, fmt.Errorf("invalid image policy defaultAction %q (expected allow or deny)", policy.DefaultAction)
	}

	rules := make(map[string]string, len(policy.Rules))
	for i, rule := range policy.Rules {
		source, err := Normalize(rule.Source)
		if err != nil {
			return nil, fmt.Errorf("image policy rule %d source: %w", i, err)
		}
		if _, exists := rules[source]; exists {
			return nil, fmt.Errorf("image policy rule %d duplicates normalized source %q", i, source)
		}

		target := source
		if strings.TrimSpace(rule.Target) != "" {
			target, err = Normalize(rule.Target)
			if err != nil {
				return nil, fmt.Errorf("image policy rule %d target: %w", i, err)
			}
		}
		rules[source] = target
	}

	return &resolver{enabled: true, allowUnmatched: allowUnmatched, rules: rules}, nil
}

// Normalize converts familiar Docker names into unambiguous references. For
// example, postgres:16 becomes docker.io/library/postgres:16 and an omitted tag
// becomes :latest. Digests are preserved.
func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("image reference is empty")
	}

	named, err := reference.ParseNormalizedNamed(raw)
	if err != nil {
		return "", fmt.Errorf("invalid image reference %q: %w", raw, err)
	}
	return reference.TagNameOnly(named).String(), nil
}

func (r *resolver) Enabled() bool {
	return r.enabled
}

func (r *resolver) Resolve(requested string) (string, error) {
	if !r.enabled {
		if strings.TrimSpace(requested) == "" {
			return "", errors.New("image reference is empty")
		}
		return requested, nil
	}

	normalized, err := Normalize(requested)
	if err != nil {
		return "", err
	}
	if target, ok := r.rules[normalized]; ok {
		return target, nil
	}
	if r.allowUnmatched {
		return normalized, nil
	}
	return "", &DeniedError{Requested: requested}
}

func ensureEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode image policy: multiple JSON values")
		}
		return fmt.Errorf("decode image policy: %w", err)
	}
	return nil
}
