package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNetworkPruneOnlyDeletesRequestOwnerNetworks(t *testing.T) {
	store := &containerStore{
		networks: map[string]*Network{
			builtInBridgeNetworkID: {ID: builtInBridgeNetworkID, Name: "bridge"},
			"na":                   {ID: "na", Name: "a", Owner: anonymousOwner},
			"nb":                   {ID: "nb", Name: "b", Owner: "owner-b"},
		},
		stateDir: t.TempDir(),
	}
	r := httptest.NewRequest(http.MethodPost, "/networks/prune", nil)
	w := httptest.NewRecorder()
	handleNetworksPrune(w, r, store)
	if w.Code != http.StatusOK {
		t.Fatalf("prune status = %d, want 200", w.Code)
	}
	if _, ok := store.networks["na"]; ok {
		t.Fatal("request owner's network was not pruned")
	}
	if _, ok := store.networks["nb"]; !ok {
		t.Fatal("another owner's network was pruned")
	}
}

func TestResourceOwnershipFiltersLists(t *testing.T) {
	store := &containerStore{
		containers: map[string]*Container{
			"a":   {ID: "a", Owner: "owner-a"},
			"b":   {ID: "b", Owner: "owner-b"},
			"old": {ID: "old"},
		},
		networks: map[string]*Network{
			builtInBridgeNetworkID: {ID: builtInBridgeNetworkID, Name: "bridge"},
			"na":                   {ID: "na", Name: "a", Owner: "owner-a"},
			"nb":                   {ID: "nb", Name: "b", Owner: "owner-b"},
		},
	}
	if got := len(store.listContainersForOwner("owner-a")); got != 1 {
		t.Fatalf("owner-a container count = %d, want 1", got)
	}
	if got := len(store.listContainersForOwner(anonymousOwner)); got != 1 {
		t.Fatalf("anonymous container count = %d, want 1", got)
	}
	if got := len(store.listNetworksForOwner("owner-a")); got != 2 {
		t.Fatalf("owner-a network count = %d, want bridge plus one owned network", got)
	}
	if _, ok := store.findContainerForOwner("b", "owner-a"); ok {
		t.Fatal("owner-a unexpectedly resolved owner-b container")
	}
	if _, ok := store.findNetworkForOwner("nb", "owner-a"); ok {
		t.Fatal("owner-a unexpectedly resolved owner-b network")
	}
	if _, ok := store.findNetworkForOwner("bridge", "owner-a"); !ok {
		t.Fatal("built-in bridge should be shared")
	}
}

func TestPeerDiscoveryDoesNotCrossOwners(t *testing.T) {
	store := &containerStore{
		containers: map[string]*Container{
			"a": {ID: "a", Owner: "owner-a", K8sPodIP: "10.0.0.1"},
			"b": {ID: "b", Owner: "owner-b", K8sPodIP: "10.0.0.2"},
			"c": {ID: "c", Owner: "owner-a", K8sPodIP: "10.0.0.3"},
		},
		networks: map[string]*Network{
			builtInBridgeNetworkID: {
				ID: builtInBridgeNetworkID,
				Containers: map[string]*NetworkEndpoint{
					"a": {Name: "a", Aliases: []string{"alias-a"}},
					"b": {Name: "b", Aliases: []string{"alias-b"}},
					"c": {Name: "c", Aliases: []string{"alias-c"}},
				},
			},
		},
	}
	aliases := store.peerHostAliasesForContainer("a")
	if _, ok := aliases["alias-b"]; ok {
		t.Fatal("cross-owner peer alias was exposed")
	}
	if got := aliases["alias-c"]; got != "10.0.0.3" {
		t.Fatalf("same-owner peer alias = %q, want 10.0.0.3", got)
	}
	view := store.networkViewForOwner(store.networks[builtInBridgeNetworkID], "owner-a")
	if len(view.Containers) != 2 || view.Containers["b"] != nil {
		t.Fatalf("owner-a bridge view exposed wrong endpoints: %#v", view.Containers)
	}
}

func TestStrictMTLSAndCertificateScopedOwnership(t *testing.T) {
	caCert, caKey, caPEM := newTestCA(t)
	serverCert, _ := newSignedTestCertificate(t, caCert, caKey, "sidewhale", true)
	clientACert, clientALeaf := newSignedTestCertificate(t, caCert, caKey, "client-a", false)
	clientBCert, clientBLeaf := newSignedTestCertificate(t, caCert, caKey, "client-b", false)

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := loadServerTLSConfig(caPath)
	if err != nil {
		t.Fatalf("loadServerTLSConfig: %v", err)
	}
	tlsConfig.Certificates = []tls.Certificate{serverCert}

	ownerA := ownerForCertificate(clientALeaf)
	ownerB := ownerForCertificate(clientBLeaf)
	store := &containerStore{
		containers: map[string]*Container{
			"a": {ID: "a", Owner: ownerA},
			"b": {ID: "b", Owner: ownerB},
		},
		networks: map[string]*Network{},
		execs: map[string]*ExecInstance{
			"exec-a": {ID: "exec-a", Owner: ownerA, ContainerID: "a"},
		},
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewUnstartedServer(resourceOwnershipMiddleware(store, next))
	srv.TLS = tlsConfig
	srv.StartTLS()
	defer srv.Close()

	clientA := testMTLSClient(caCert, clientACert)
	defer clientA.CloseIdleConnections()
	clientB := testMTLSClient(caCert, clientBCert)
	defer clientB.CloseIdleConnections()

	assertHTTPStatus(t, clientA, srv.URL+"/containers/a/json", http.StatusNoContent)
	assertHTTPStatus(t, clientA, srv.URL+"/containers/b/json", http.StatusNotFound)
	assertHTTPStatus(t, clientB, srv.URL+"/containers/b/json", http.StatusNoContent)
	assertHTTPStatus(t, clientB, srv.URL+"/exec/exec-a/json", http.StatusNotFound)

	noCertClient := testMTLSClient(caCert, tls.Certificate{})
	defer noCertClient.CloseIdleConnections()
	if _, err := noCertClient.Get(srv.URL + "/containers/a/json"); err == nil {
		t.Fatal("request without a client certificate unexpectedly completed")
	}
}

func ownerForCertificate(cert *x509.Certificate) string {
	r := &http.Request{TLS: &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}}
	return requestOwner(r)
}

func newTestCA(t *testing.T) (*x509.Certificate, ed25519.PrivateKey, []byte) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "sidewhale-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func newSignedTestCertificate(t *testing.T, ca *x509.Certificate, caKey ed25519.PrivateKey, name string, server bool) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.DNSNames = []string{"example.com"}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, pub, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{
		Certificate: [][]byte{der, ca.Raw},
		PrivateKey:  key,
		Leaf:        leaf,
	}
	return cert, leaf
}

func testMTLSClient(ca *x509.Certificate, cert tls.Certificate) *http.Client {
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	tlsConfig := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	if len(cert.Certificate) > 0 {
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
}

func assertHTTPStatus(t *testing.T, client *http.Client, url string, want int) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("GET %s status = %d, want %d", url, resp.StatusCode, want)
	}
}
