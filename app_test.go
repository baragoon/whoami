package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const websocketTestTimeout = 5 * time.Second

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
