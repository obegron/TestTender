package image

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		"postgres:16":                      "docker.io/library/postgres:16",
		"redis":                            "docker.io/library/redis:latest",
		"docker.io/library/redis:7-alpine": "docker.io/library/redis:7-alpine",
		"registry.example.test/team/database@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": "registry.example.test/team/database@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	for input, want := range tests {
		got, err := Normalize(input)
		if err != nil {
			t.Fatalf("Normalize(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDecodeDenyAndRewrite(t *testing.T) {
	policy := `{
		"version":"v1",
		"defaultAction":"deny",
		"rules":[
			{"source":"postgres:16","target":"registry.example.test/testcontainers/postgres:16"},
			{"source":"redis:7-alpine"}
		]
	}`
	resolver, err := Decode(strings.NewReader(policy))
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolver.Resolve("postgres:16")
	if err != nil {
		t.Fatal(err)
	}
	if want := "registry.example.test/testcontainers/postgres:16"; got != want {
		t.Fatalf("resolved image = %q, want %q", got, want)
	}

	got, err = resolver.Resolve("docker.io/library/redis:7-alpine")
	if err != nil {
		t.Fatal(err)
	}
	if want := "docker.io/library/redis:7-alpine"; got != want {
		t.Fatalf("allowed image = %q, want %q", got, want)
	}

	_, err = resolver.Resolve("alpine:3.23")
	var denied *DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected DeniedError, got %v", err)
	}
}

func TestDecodeAllowNormalizesUnmatched(t *testing.T) {
	resolver, err := Decode(strings.NewReader(`{"version":"v1","defaultAction":"allow","rules":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolver.Resolve("alpine")
	if err != nil {
		t.Fatal(err)
	}
	if want := "docker.io/library/alpine:latest"; got != want {
		t.Fatalf("resolved image = %q, want %q", got, want)
	}
}

func TestDecodeRejectsInvalidPolicies(t *testing.T) {
	tests := []string{
		`{}`,
		`{"version":"v2","defaultAction":"deny"}`,
		`{"version":"v1","defaultAction":"maybe"}`,
		`{"version":"v1","defaultAction":"deny","unknown":true}`,
		`{"version":"v1","defaultAction":"deny","rules":[{"source":"postgres:16"},{"source":"docker.io/library/postgres:16"}]}`,
		`{"version":"v1","defaultAction":"deny"} {}`,
	}
	for _, input := range tests {
		if _, err := Decode(strings.NewReader(input)); err == nil {
			t.Errorf("Decode(%s) unexpectedly succeeded", input)
		}
	}
}

func TestPassthroughPreservesRequestedReference(t *testing.T) {
	resolver := Passthrough()
	got, err := resolver.Resolve("postgres:16")
	if err != nil {
		t.Fatal(err)
	}
	if got != "postgres:16" {
		t.Fatalf("resolved image = %q", got)
	}
}
