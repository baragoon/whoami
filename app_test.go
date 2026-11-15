package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

func Test_echoHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(echoHandler))
	defer server.Close()

	conn, response, err := websocket.Dial(
		context.Background(),
		"ws"+strings.TrimPrefix(server.URL, "http"),
		&websocket.DialOptions{
			HTTPHeader: http.Header{
				"Origin": []string{"https://cross-origin.example"},
			},
		},
	)
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
			writeErr := conn.Write(context.Background(), test.messageType, test.message)
			if writeErr != nil {
				t.Fatalf("websocket.Conn.Write() error = %v", writeErr)
			}

			messageType, message, err := conn.Read(context.Background())
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
