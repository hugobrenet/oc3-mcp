package testutil

import (
	"crypto/rand"
	"crypto/rsa"
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

const (
	CollectorLogin    = "mcp@example.com"
	CollectorPassword = "example-password"
)

type Collector struct {
	Server *httptest.Server
	CAFile string
	// URL is the base URL of the fake collector API.
	URL string
}

// NewCollector serves handler over TLS under /api, trusted through a
// temporary CA file. Requests without the test credentials get 401.
func NewCollector(t testing.TB, handler http.Handler) Collector {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Example CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Example collector"}, BasicConstraintsValid: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		login, password, ok := r.BasicAuth()
		if !ok || login != CollectorLogin || password != CollectorPassword {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"text":"strategies/union: [invalid credentials]"}`))
			return
		}
		handler.ServeHTTP(w, r)
	})))
	server := httptest.NewUnstartedServer(mux)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: leafKey}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "example-ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return Collector{Server: server, CAFile: path, URL: server.URL + "/api"}
}

// WriteNetrc writes a credentials file with mode for host.
func WriteNetrc(t testing.TB, host string, login string, password string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "collector.netrc")
	if err := os.WriteFile(path, []byte("machine "+host+" login "+login+" password "+password+"\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}
