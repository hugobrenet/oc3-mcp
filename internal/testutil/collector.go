package testutil

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opensvc/oc3-mcp/internal/auth"
)

const (
	TokenIssuer  = "https://sso.example.com/application/o/collector/"
	TokenSubject = "user-subject"
)

// Token is an access token of the shape the MCP accepts, valid for a day. Its
// signature is not genuine: the fake collector compares the whole token.
var Token = NewToken(TokenIssuer, TokenSubject, time.Now().Add(24*time.Hour))

// NewToken returns an access token with the given claims and a fake
// signature.
func NewToken(issuer string, subject string, expires time.Time) string {
	segment := func(value any) string {
		data, _ := json.Marshal(value)
		return base64.RawURLEncoding.EncodeToString(data)
	}
	return segment(map[string]string{"alg": "RS256", "kid": "example-key", "typ": "JWT"}) + "." +
		segment(map[string]any{"iss": issuer, "sub": subject, "aud": "collector", "exp": expires.Unix()}) + "." +
		base64.RawURLEncoding.EncodeToString([]byte("not-a-signature"))
}

// CallerContext returns a context carrying Token as the caller's token.
func CallerContext(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel, err := auth.NewContext(t.Context(), Token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	return ctx
}

type Collector struct {
	Server *httptest.Server
	CAFile string
	// URL is the base URL of the fake collector API.
	URL string
}

// NewCollector serves handler over TLS under /api, trusted through a
// temporary CA file. Requests without Token get 401.
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
		if r.Header.Get("Authorization") != "Bearer "+Token {
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
