package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	k8sOwnerLabelKey         = "sidewhale.owner-id"
	k8sManagedLabelKey       = "sidewhale.managed"
	k8sOwnerPolicyAppName    = "sidewhale-owner-network"
	k8sOwnerPolicyNamePrefix = "sidewhale-owner-"
)

type k8sNetworkPolicy struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
}

type k8sNetworkPolicyList struct {
	Items []k8sNetworkPolicy `json:"items"`
}

func ownerK8sID(owner string) string {
	sum := sha256.Sum256([]byte(canonicalOwner(owner)))
	return "o-" + hex.EncodeToString(sum[:16])
}

func ownerNetworkPolicyName(owner string) string {
	return k8sOwnerPolicyNamePrefix + ownerK8sID(owner)
}

func ownerNetworkPolicyObject(namespace, owner string) map[string]interface{} {
	ownerID := ownerK8sID(owner)
	return map[string]interface{}{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata": map[string]interface{}{
			"name":      ownerNetworkPolicyName(owner),
			"namespace": namespace,
			"labels": map[string]string{
				"app.kubernetes.io/name": k8sOwnerPolicyAppName,
				k8sManagedLabelKey:       "true",
				k8sOwnerLabelKey:         ownerID,
			},
		},
		"spec": map[string]interface{}{
			"podSelector": map[string]interface{}{
				"matchLabels": map[string]string{
					k8sManagedLabelKey: "true",
					k8sOwnerLabelKey:   ownerID,
				},
			},
			"policyTypes": []string{"Ingress"},
			"ingress": []map[string]interface{}{
				{
					"from": []map[string]interface{}{
						{
							"podSelector": map[string]interface{}{
								"matchLabels": map[string]string{"app": "sidewhale"},
							},
						},
						{
							"podSelector": map[string]interface{}{
								"matchLabels": map[string]string{
									k8sManagedLabelKey: "true",
									k8sOwnerLabelKey:   ownerID,
								},
							},
						},
					},
				},
			},
		},
	}
}

func (k *k8sClient) ensureOwnerNetworkPolicy(ctx context.Context, owner string) error {
	body, err := json.Marshal(ownerNetworkPolicyObject(k.namespace, owner))
	if err != nil {
		return err
	}
	query := url.Values{}
	query.Set("fieldManager", "sidewhale")
	query.Set("force", "true")
	path := "/apis/networking.k8s.io/v1/namespaces/" + url.PathEscape(k.namespace) +
		"/networkpolicies/" + url.PathEscape(ownerNetworkPolicyName(owner)) + "?" + query.Encode()
	resp, err := k.doRequest(ctx, http.MethodPatch, path, body, "application/apply-patch+yaml")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("apply network policy failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func (k *k8sClient) patchPodOwnerLabel(ctx context.Context, namespace, podName, owner string) error {
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		ns = k.namespace
	}
	body, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]string{k8sOwnerLabelKey: ownerK8sID(owner)},
		},
	})
	if err != nil {
		return err
	}
	path := "/api/v1/namespaces/" + url.PathEscape(ns) + "/pods/" + url.PathEscape(podName)
	resp, err := k.doRequest(ctx, http.MethodPatch, path, body, "application/merge-patch+json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("patch pod owner label failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func (k *k8sClient) listOwnerNetworkPolicies(ctx context.Context) ([]k8sNetworkPolicy, error) {
	query := url.Values{}
	query.Set("labelSelector", "app.kubernetes.io/name="+k8sOwnerPolicyAppName)
	path := "/apis/networking.k8s.io/v1/namespaces/" + url.PathEscape(k.namespace) +
		"/networkpolicies?" + query.Encode()
	resp, err := k.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("list owner network policies failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var out k8sNetworkPolicyList
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (k *k8sClient) deleteOwnerNetworkPolicy(ctx context.Context, owner string) error {
	return k.deleteNetworkPolicy(ctx, ownerNetworkPolicyName(owner))
}

func (k *k8sClient) deleteNetworkPolicy(ctx context.Context, name string) error {
	path := "/apis/networking.k8s.io/v1/namespaces/" + url.PathEscape(k.namespace) +
		"/networkpolicies/" + url.PathEscape(name)
	resp, err := k.doJSON(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("delete owner network policy failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}
