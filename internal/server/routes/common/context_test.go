package common

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/obegron/testtender/internal/model/types"
	secretpolicy "github.com/obegron/testtender/internal/policy/secret"
)

func TestValidateContainerRequestRejectsDockerSocket(t *testing.T) {
	tests := []*types.Container{
		{Binds: []string{"/var/run/docker.sock:/var/run/docker.sock"}},
		{Mounts: []types.Mount{{Source: "/tmp/docker.sock", Target: "/var/run/docker.sock"}}},
	}
	for _, container := range tests {
		err := ValidateContainerRequest(container)
		if err == nil || !strings.Contains(err.Error(), "Kubernetes Pods") {
			t.Fatalf("expected Kubernetes-only rejection, got %v", err)
		}
	}
}

func TestValidateContainerRequestAllowsOrdinaryCopyMount(t *testing.T) {
	container := &types.Container{Binds: []string{"/workspace/fixture:/opt/fixture:ro"}}
	if err := ValidateContainerRequest(container); err != nil {
		t.Fatalf("unexpected rejection: %v", err)
	}
}

func TestContextRouterValidatesSecretReferencesAtCreate(t *testing.T) {
	policy, err := secretpolicy.New([]string{"integration-database"})
	if err != nil {
		t.Fatal(err)
	}
	router := &ContextRouter{SecretPolicy: policy}

	allowed := &types.Container{Labels: map[string]string{
		secretpolicy.EnvLabelPrefix + "PASSWORD": "integration-database:password",
	}}
	if err := router.ValidateContainerRequest(allowed); err != nil {
		t.Fatalf("allowed reference rejected: %v", err)
	}

	denied := &types.Container{Labels: map[string]string{
		secretpolicy.EnvLabelPrefix + "PASSWORD": "production-database:password",
	}}
	err = router.ValidateContainerRequest(denied)
	if err == nil {
		t.Fatal("non-allowlisted reference accepted")
	}
	if status := ContainerRequestErrorStatus(err); status != 403 {
		t.Fatalf("status = %d, want 403", status)
	}
}

func TestContainerStateResponseIncludesWorkloadResult(t *testing.T) {
	started := time.Date(2026, time.August, 16, 10, 11, 12, 123, time.FixedZone("test", 2*60*60))
	finished := started.Add(2 * time.Second)
	container := &types.Container{
		Completed:  true,
		OOMKilled:  true,
		ExitCode:   137,
		StateError: "kubelet message",
		Started:    started,
		Finished:   finished,
	}

	got := ContainerStateResponse(container, "network lookup failed")
	want := gin.H{
		"Health":     gin.H{"Status": "unhealthy"},
		"Running":    false,
		"Status":     "exited",
		"Paused":     false,
		"Restarting": false,
		"OOMKilled":  true,
		"Dead":       false,
		"StartedAt":  started.UTC().Format(time.RFC3339Nano),
		"FinishedAt": finished.UTC().Format(time.RFC3339Nano),
		"ExitCode":   int64(137),
		"Error":      "network lookup failed; kubelet message",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("state response mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}
