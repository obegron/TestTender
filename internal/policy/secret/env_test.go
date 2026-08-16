package secret

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestEnvVars(t *testing.T) {
	policy, err := New([]string{"integration-database", "integration-broker"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := policy.EnvVars(map[string]string{
		EnvLabelPrefix + "DB_USER":     "integration-database:username",
		EnvLabelPrefix + "DB_PASSWORD": "integration-database:password",
		"ordinary":                     "label",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d references, want 2", len(got))
	}
	if got[0].Name != "DB_PASSWORD" || got[0].ValueFrom.SecretKeyRef.Name != "integration-database" || got[0].ValueFrom.SecretKeyRef.Key != "password" {
		t.Fatalf("unexpected first reference: %#v", got[0])
	}
	if got[1].Name != "DB_USER" || got[1].ValueFrom.SecretKeyRef.Key != "username" {
		t.Fatalf("unexpected second reference: %#v", got[1])
	}
}

func TestEnvVarsRejectsUnsafeReferences(t *testing.T) {
	policy, err := New([]string{"integration-database"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		labels   map[string]string
		existing []corev1.EnvVar
		contains string
	}{
		{
			name:     "unlisted Secret",
			labels:   map[string]string{EnvLabelPrefix + "PASSWORD": "production-database:password"},
			contains: "not allowed",
		},
		{
			name:     "missing key",
			labels:   map[string]string{EnvLabelPrefix + "PASSWORD": "integration-database"},
			contains: "expected <secret-name>:<secret-key>",
		},
		{
			name:     "invalid environment name",
			labels:   map[string]string{EnvLabelPrefix + "DB PASSWORD": "integration-database:password"},
			contains: "invalid Secret environment variable name",
		},
		{
			name:     "ordinary environment collision",
			labels:   map[string]string{EnvLabelPrefix + "PASSWORD": "integration-database:password"},
			existing: []corev1.EnvVar{{Name: "PASSWORD", Value: "plain"}},
			contains: "conflicts with an ordinary environment variable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := policy.EnvVars(tt.labels, tt.existing)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("error = %v, want text %q", err, tt.contains)
			}
		})
	}
}

func TestFiles(t *testing.T) {
	policy, err := New([]string{"integration-database", "integration-client"})
	if err != nil {
		t.Fatal(err)
	}
	volumes, mounts, err := policy.Files(map[string]string{
		FileLabelPrefix + "db_password": "integration-database:password",
		FileLabelPrefix + "client.pem":  "integration-client:tls.crt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(volumes) != 1 || volumes[0].Projected == nil || len(volumes[0].Projected.Sources) != 2 {
		t.Fatalf("unexpected projected volume: %#v", volumes)
	}
	if len(mounts) != 1 || mounts[0].MountPath != MountPath || !mounts[0].ReadOnly {
		t.Fatalf("unexpected mount: %#v", mounts)
	}
	if mode := volumes[0].Projected.DefaultMode; mode == nil || *mode != 0440 {
		t.Fatalf("default mode = %v, want 0440", mode)
	}
	first := volumes[0].Projected.Sources[0].Secret
	if first.Name != "integration-client" || len(first.Items) != 1 || first.Items[0].Path != "client.pem" {
		t.Fatalf("unexpected first projection: %#v", first)
	}
	second := volumes[0].Projected.Sources[1].Secret
	if second.Name != "integration-database" || second.Items[0].Key != "password" || second.Items[0].Path != "db_password" {
		t.Fatalf("unexpected second projection: %#v", second)
	}
}

func TestFilesRejectsUnsafeReferences(t *testing.T) {
	policy, err := New([]string{"integration-database"})
	if err != nil {
		t.Fatal(err)
	}
	for _, labels := range []map[string]string{
		{FileLabelPrefix + "..": "integration-database:password"},
		{FileLabelPrefix + "password": "production-database:password"},
		{FileLabelPrefix + "nested/path": "integration-database:password"},
	} {
		if _, _, err := policy.Files(labels); err == nil {
			t.Fatalf("unsafe labels accepted: %#v", labels)
		}
	}
}

func TestNewRejectsInvalidAllowedSecret(t *testing.T) {
	if _, err := New([]string{"UPPERCASE"}); err == nil {
		t.Fatal("expected invalid Secret allowlist entry to fail")
	}
}

func TestEmptyAllowlistFailsClosed(t *testing.T) {
	policy, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = policy.EnvVars(map[string]string{
		EnvLabelPrefix + "PASSWORD": "integration-database:password",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("error = %v, want deny", err)
	}
}
