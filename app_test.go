package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const (
	websocketTestTimeout = 5 * time.Second
	privateRemoteAddr    = "192.168.90.254:48130"
	publicIPv4           = "8.8.8.8"
	publicRemoteAddr     = publicIPv4 + ":443"
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
