package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
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
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const healthCheckTestHost = "127.0.0.1"

const (
	healthCheckClientCertFlag = "--healthcheck-client-cert="
	healthCheckClientKeyFlag  = "--healthcheck-client-key="
	healthCheckServerCAFlag   = "--healthcheck-server-ca="
	websocketTestTimeout      = 5 * time.Second
	privateRemoteAddr         = "192.168.90.254:48130"
	publicIPv4                = "8.8.8.8"
	publicRemoteAddr          = publicIPv4 + ":443"
)

func Test_echoHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(echoHandler))
	defer server.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), websocketTestTimeout)

	conn, response, err := websocket.Dial(
		dialCtx,
		"ws"+strings.TrimPrefix(server.URL, "http"),
		&websocket.DialOptions{
			HTTPHeader: http.Header{
				"Origin": []string{"https://cross-origin.example"},
			},
		},
	)

	cancelDial()

	if response != nil && response.Body != nil {
		defer func() {
			closeErr := response.Body.Close()
			if closeErr != nil {
				t.Errorf("response.Body.Close() error = %v", closeErr)
			}
		}()
	}

	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}

	conn.SetReadLimit(512 << 20)

	defer func() {
		closeErr := conn.Close(websocket.StatusNormalClosure, "")
		if closeErr != nil {
			t.Errorf("websocket.Conn.Close() error = %v", closeErr)
		}
	}()

	tests := []struct {
		name        string
		messageType websocket.MessageType
		message     []byte
	}{
		{
			name:        "text",
			messageType: websocket.MessageText,
			message:     []byte("hello"),
		},
		{
			name:        "binary",
			messageType: websocket.MessageBinary,
			message:     []byte{0, 1, 2},
		},
		{
			name:        "message larger than default read limit",
			messageType: websocket.MessageBinary,
			message:     make([]byte, 32*1024+1),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), websocketTestTimeout)
			defer cancel()

			writeErr := conn.Write(ctx, test.messageType, test.message)
			if writeErr != nil {
				t.Fatalf("websocket.Conn.Write() error = %v", writeErr)
			}

			messageType, message, err := conn.Read(ctx)
			if err != nil {
				t.Fatalf("websocket.Conn.Read() error = %v", err)
			}

			if messageType != test.messageType {
				t.Errorf("message type = %v, want %v", messageType, test.messageType)
			}

			if !bytes.Equal(message, test.message) {
				t.Errorf("message = %v, want %v", message, test.message)
			}
		})
	}
}

func Test_checkHealth(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "healthy",
			statusCode: http.StatusOK,
		},
		{
			name:       "unhealthy",
			statusCode: http.StatusServiceUnavailable,
			wantErr:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
			}))
			defer server.Close()

			err := checkHealth(server.URL, nil)
			if (err != nil) != test.wantErr {
				t.Errorf("checkHealth() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func Test_runHealthCheckUsesConfiguredPort(t *testing.T) {
	t.Setenv("WHOAMI_CACERT", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("net.SplitHostPort() error = %v", err)
	}

	err = runHealthCheck([]string{
		"--port=" + port,
		"--healthcheck-host=" + host,
		"--cert=",
		"--key=",
		healthCheckClientCertFlag,
		healthCheckClientKeyFlag,
		healthCheckServerCAFlag,
	})
	if err != nil {
		t.Errorf("runHealthCheck() error = %v", err)
	}
}

func Test_runHealthCheckUsesTLSAndClientCertificate(t *testing.T) {
	t.Setenv("WHOAMI_CACERT", "")

	serverCredentials := writeHealthCheckCertificates(t, x509.ExtKeyUsageServerAuth)
	clientCredentials := writeHealthCheckCertificates(t, x509.ExtKeyUsageClientAuth)

	serverCert, err := tls.LoadX509KeyPair(serverCredentials.cert, serverCredentials.key)
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair() error = %v", err)
	}

	clientCAPEM, err := os.ReadFile(clientCredentials.ca)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	clientCAs := x509.NewCertPool()

	if !clientCAs.AppendCertsFromPEM(clientCAPEM) {
		t.Fatal("AppendCertsFromPEM() returned false")
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		MinVersion:   tls.VersionTLS12,
	}

	server.StartTLS()

	defer server.Close()

	host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "https://"))
	if err != nil {
		t.Fatalf("net.SplitHostPort() error = %v", err)
	}

	checkArgs := []string{
		"--port=" + port,
		"--healthcheck-host=" + host,
		"--cert=" + serverCredentials.cert,
		"--key=" + serverCredentials.key,
		healthCheckClientCertFlag + clientCredentials.cert,
		healthCheckClientKeyFlag + clientCredentials.key,
	}

	untrustedArgs := make([]string, len(checkArgs)+1)
	copy(untrustedArgs, checkArgs)
	untrustedArgs[len(checkArgs)] = healthCheckServerCAFlag

	err = runHealthCheck(untrustedArgs)
	if err == nil {
		t.Fatal("runHealthCheck() succeeded without trusting the server certificate")
	}

	trustedArgs := make([]string, len(checkArgs)+1)
	copy(trustedArgs, checkArgs)
	trustedArgs[len(checkArgs)] = healthCheckServerCAFlag + serverCredentials.ca

	err = runHealthCheck(trustedArgs)
	if err != nil {
		t.Errorf("runHealthCheck() with mutual TLS error = %v", err)
	}
}

func Test_runHealthCheckAllowsServerOnlyCertificate(t *testing.T) {
	t.Setenv("WHOAMI_CACERT", "")

	serverCredentials := writeHealthCheckCertificates(t, x509.ExtKeyUsageServerAuth)

	serverCert, err := tls.LoadX509KeyPair(serverCredentials.cert, serverCredentials.key)
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair() error = %v", err)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
	}

	server.StartTLS()

	defer server.Close()

	host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "https://"))
	if err != nil {
		t.Fatalf("net.SplitHostPort() error = %v", err)
	}

	err = runHealthCheck([]string{
		"--port=" + port,
		"--healthcheck-host=" + host,
		"--cert=" + serverCredentials.cert,
		"--key=" + serverCredentials.key,
		healthCheckClientCertFlag,
		healthCheckClientKeyFlag,
		healthCheckServerCAFlag + serverCredentials.ca,
	})
	if err != nil {
		t.Errorf("runHealthCheck() with server-only certificate error = %v", err)
	}
}

func Test_runHealthCheckRequiresClientCredentialsForMTLS(t *testing.T) {
	t.Setenv("WHOAMI_CACERT", "client-ca.pem")

	err := runHealthCheck([]string{
		"--cert=server.crt",
		"--key=server.key",
		healthCheckClientCertFlag,
		healthCheckClientKeyFlag,
		healthCheckServerCAFlag,
	})
	if err == nil || !strings.Contains(err.Error(), "WHOAMI_HEALTHCHECK_CLIENT_CERT") {
		t.Errorf("runHealthCheck() error = %v, want mTLS client-credential guidance", err)
	}
}

type healthCheckTestCertificates struct {
	cert string
	key  string
	ca   string
}

func writeHealthCheckCertificates(t *testing.T, usage x509.ExtKeyUsage) healthCheckTestCertificates {
	t.Helper()

	now := time.Now()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "health check test CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("x509.CreateCertificate() for CA error = %v", err)
	}

	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("x509.ParseCertificate() error = %v", err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() for leaf error = %v", err)
	}

	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: healthCheckTestHost},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP(healthCheckTestHost)},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}

	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("x509.CreateCertificate() for leaf error = %v", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatalf("x509.MarshalPKCS8PrivateKey() error = %v", err)
	}

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	caFile := filepath.Join(dir, "ca.pem")
	files := []struct {
		path string
		data []byte
	}{
		{
			path: certFile,
			data: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		},
		{
			path: keyFile,
			data: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		},
		{
			path: caFile,
			data: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		},
	}

	for _, file := range files {
		err = os.WriteFile(file.path, file.data, 0o600)
		if err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}
	}

	return healthCheckTestCertificates{cert: certFile, key: keyFile, ca: caFile}
}

func Test_isInternalIP(t *testing.T) {
	tests := []struct {
		ip       string
		internal bool
	}{
		{ip: "127.0.0.1", internal: true},
		{ip: "::1", internal: true},
		{ip: "192.168.90.9", internal: true},
		{ip: "fd4a:bfb9:100e:90::9", internal: true},
		{ip: "fe80::90d4:c4ff:fed8:a3cd", internal: true},
		{ip: publicIPv4, internal: false},
	}

	for _, test := range tests {
		t.Run(test.ip, func(t *testing.T) {
			ip := net.ParseIP(test.ip)
			if ip == nil {
				t.Fatalf("net.ParseIP(%q) returned nil", test.ip)
			}

			if got := isInternalIP(ip); got != test.internal {
				t.Errorf("isInternalIP(%q) = %t, want %t", test.ip, got, test.internal)
			}
		})
	}
}

func Test_filterRemoteAddr(t *testing.T) {
	tests := []struct {
		name         string
		remoteAddr   string
		hideInternal bool
		want         string
	}{
		{
			name:         "hides IPv4 private address and port",
			remoteAddr:   privateRemoteAddr,
			hideInternal: true,
		},
		{
			name:         "hides IPv6 loopback",
			remoteAddr:   "[::1]:48130",
			hideInternal: true,
		},
		{
			name:         "hides scoped IPv6 link-local address",
			remoteAddr:   "[fe80::90d4:c4ff:fed8:a3cd%eth0]:48130",
			hideInternal: true,
		},
		{
			name:         "keeps public address",
			remoteAddr:   publicRemoteAddr,
			hideInternal: true,
			want:         publicRemoteAddr,
		},
		{
			name:       "keeps address when disabled",
			remoteAddr: privateRemoteAddr,
			want:       privateRemoteAddr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := filterRemoteAddr(test.remoteAddr, test.hideInternal); got != test.want {
				t.Errorf("filterRemoteAddr() = %q, want %q", got, test.want)
			}
		})
	}
}

func Test_filterIPsHidesInternalAddresses(t *testing.T) {
	input := []string{
		"127.0.0.1",
		"::1",
		"192.168.90.9",
		"fd4a:bfb9:100e:90::9",
		"fe80::90d4:c4ff:fed8:a3cd",
		publicIPv4,
		"2001:4860:4860::8888",
	}

	ips := make([]net.IP, 0, len(input))
	for _, value := range input {
		ip := net.ParseIP(value)
		if ip == nil {
			t.Fatalf("net.ParseIP(%q) returned nil", value)
		}

		ips = append(ips, ip)
	}

	got := filterIPs(ips, true)

	want := []string{publicIPv4, "2001:4860:4860::8888"}
	if len(got) != len(want) {
		t.Fatalf("filterIPs() = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("filterIPs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
