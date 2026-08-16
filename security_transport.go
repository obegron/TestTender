package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const anonymousOwner = "anonymous"

func canonicalOwner(owner string) string {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return anonymousOwner
	}
	return owner
}

func requestOwner(r *http.Request) string {
	if r == nil || r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return anonymousOwner
	}
	spki := r.TLS.PeerCertificates[0].RawSubjectPublicKeyInfo
	if len(spki) == 0 {
		return anonymousOwner
	}
	sum := sha256.Sum256(spki)
	return "mtls:" + hex.EncodeToString(sum[:])
}

func ownerMatches(resourceOwner, requestOwner string) bool {
	return canonicalOwner(resourceOwner) == canonicalOwner(requestOwner)
}

func loadServerTLSConfig(clientCAFile string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	clientCAFile = strings.TrimSpace(clientCAFile)
	if clientCAFile == "" {
		return cfg, nil
	}
	pem, err := os.ReadFile(clientCAFile)
	if err != nil {
		return nil, fmt.Errorf("read TLS client CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("TLS client CA contains no certificates")
	}
	cfg.ClientCAs = pool
	cfg.ClientAuth = tls.RequireAndVerifyClientCert
	return cfg, nil
}

// resourceOwnershipMiddleware hides resources belonging to another client.
// It runs after API-version path normalization.
func resourceOwnershipMiddleware(store *containerStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := requestOwner(r)
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if len(parts) >= 2 {
			switch parts[0] {
			case "containers":
				if parts[1] != "create" && parts[1] != "json" && parts[1] != "prune" {
					if _, ok := store.findContainerForOwner(parts[1], owner); !ok {
						writeError(w, http.StatusNotFound, "container not found")
						return
					}
				}
			case "networks":
				if parts[1] != "create" && parts[1] != "prune" {
					if _, ok := store.findNetworkForOwner(parts[1], owner); !ok {
						writeError(w, http.StatusNotFound, "network not found")
						return
					}
				}
			case "exec":
				if inst, ok := store.findExec(parts[1]); !ok || !ownerMatches(inst.Owner, owner) {
					writeError(w, http.StatusNotFound, "exec instance not found")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
