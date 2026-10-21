package notify

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSlackAndFanout(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := (Slack{Webhook: server.URL, Client: server.Client()}).Notify(context.Background(), Message{Subject: "spike", Body: "up"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "spike") {
		t.Fatalf("body = %s", body)
	}
	err := Fanout(context.Background(), []Notifier{
		Slack{Webhook: server.URL, Client: server.Client()},
		failNotifier{},
	}, Message{Subject: "x", Body: "y"})
	if err == nil || !strings.Contains(err.Error(), "fail") {
		t.Fatalf("err = %v", err)
	}
}

func TestEmail(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go serveSMTP(ln, done)

	err = (Email{
		Host: "127.0.0.1",
		Port: ln.Addr().(*net.TCPAddr).Port,
		From: "ripper@example.com",
		To:   "ops@example.com",
	}).Notify(context.Background(), Message{Subject: "cost\nspike", Body: "details"})
	if err != nil {
		t.Fatal(err)
	}
	<-done
}

type failNotifier struct{}

func (failNotifier) Name() string { return "fail" }

func (failNotifier) Notify(context.Context, Message) error { return errors.New("nope") }

func serveSMTP(ln net.Listener, done chan struct{}) {
	defer close(done)
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	_, _ = conn.Write([]byte("220 localhost\r\n"))
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "DATA"):
			_, _ = conn.Write([]byte("354 go\r\n"))
			for {
				part, err := reader.ReadString('\n')
				if err != nil || strings.TrimSpace(part) == "." {
					break
				}
			}
			_, _ = conn.Write([]byte("250 OK\r\n"))
		case strings.HasPrefix(cmd, "QUIT"):
			_, _ = conn.Write([]byte("221 bye\r\n"))
			return
		default:
			_, _ = conn.Write([]byte("250 localhost\r\n"))
		}
	}
}
