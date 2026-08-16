package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOwnerK8sIDIsStableOpaqueAndLabelSafe(t *testing.T) {
	a := ownerK8sID("mtls:client-a")
	if a != ownerK8sID("mtls:client-a") {
		t.Fatal("owner Kubernetes ID is not stable")
	}
	if a == ownerK8sID("mtls:client-b") {
		t.Fatal("different owners received the same Kubernetes ID")
	}
	if strings.Contains(a, "client-a") {
		t.Fatalf("owner Kubernetes ID leaks source identity: %q", a)
	}
	if len(a) > 63 || !regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`).MatchString(a) {
		t.Fatalf("owner Kubernetes ID is not label-safe: %q", a)
	}
	if ownerK8sID("") != ownerK8sID(anonymousOwner) {
		t.Fatal("empty persisted owner and anonymous owner must share an ID")
	}
}

func TestEnsureOwnerNetworkPolicyUsesServerSideApply(t *testing.T) {
	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %s, want PATCH", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/apply-patch+yaml" {
			t.Fatalf("Content-Type = %q", got)
		}
		if r.URL.Query().Get("fieldManager") != "sidewhale" || r.URL.Query().Get("force") != "true" {
			t.Fatalf("apply query = %q", r.URL.RawQuery)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := &k8sClient{baseURL: srv.URL, token: "x", namespace: "workers", http: srv.Client()}
	owner := "mtls:client-a"
	if err := client.ensureOwnerNetworkPolicy(context.Background(), owner); err != nil {
		t.Fatalf("ensureOwnerNetworkPolicy: %v", err)
	}
	metadata := got["metadata"].(map[string]interface{})
	if metadata["name"] != ownerNetworkPolicyName(owner) || metadata["namespace"] != "workers" {
		t.Fatalf("policy metadata = %#v", metadata)
	}
	assertOwnerPolicyShape(t, got, ownerK8sID(owner))
}

func TestEnsureOwnerNetworkPolicyFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	client := &k8sClient{baseURL: srv.URL, token: "x", namespace: "workers", http: srv.Client()}
	if err := client.ensureOwnerNetworkPolicy(context.Background(), "owner"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("error = %v, want status 403", err)
	}
}

func TestK8sManifestDoesNotBroadlyAllowWorkerPeers(t *testing.T) {
	f, err := os.Open("deploy/sidewhale-k8s-runtime.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	foundPolicy := false
	foundRBAC := false
	for {
		var doc map[string]interface{}
		if err := decoder.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatal(err)
		}
		metadata, _ := doc["metadata"].(map[string]interface{})
		name, _ := metadata["name"].(string)
		switch name {
		case "sidewhale-workload-ingress":
			foundPolicy = true
			spec := doc["spec"].(map[string]interface{})
			ingress := spec["ingress"].([]interface{})
			from := ingress[0].(map[string]interface{})["from"].([]interface{})
			if len(from) != 1 {
				t.Fatalf("baseline policy has %d peers, want only Sidewhale", len(from))
			}
		case "sidewhale-runtime":
			if doc["kind"] != "Role" {
				continue
			}
			rules := doc["rules"].([]interface{})
			for _, raw := range rules {
				rule := raw.(map[string]interface{})
				resources, _ := rule["resources"].([]interface{})
				for _, resource := range resources {
					if resource == "networkpolicies" {
						foundRBAC = true
					}
				}
			}
		}
	}
	if !foundPolicy {
		t.Fatal("sidewhale-workload-ingress policy not found")
	}
	if !foundRBAC {
		t.Fatal("networkpolicy RBAC rule not found")
	}
}

func assertOwnerPolicyShape(t *testing.T, policy map[string]interface{}, ownerID string) {
	t.Helper()
	spec := policy["spec"].(map[string]interface{})
	selector := spec["podSelector"].(map[string]interface{})["matchLabels"].(map[string]interface{})
	if selector[k8sManagedLabelKey] != "true" || selector[k8sOwnerLabelKey] != ownerID {
		t.Fatalf("policy selector = %#v", selector)
	}
	ingress := spec["ingress"].([]interface{})
	peers := ingress[0].(map[string]interface{})["from"].([]interface{})
	if len(peers) != 2 {
		t.Fatalf("policy peers = %d, want Sidewhale and same owner", len(peers))
	}
	sameOwner := peers[1].(map[string]interface{})["podSelector"].(map[string]interface{})["matchLabels"].(map[string]interface{})
	if sameOwner[k8sManagedLabelKey] != "true" || sameOwner[k8sOwnerLabelKey] != ownerID {
		t.Fatalf("same-owner peer selector = %#v", sameOwner)
	}
}
