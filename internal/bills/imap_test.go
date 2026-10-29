package bills

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIMAPDoesNotMutateMail(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var mu sync.Mutex
	var cmds []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		_, _ = conn.Write([]byte("* OK IMAP4rev1 ready\r\n"))
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			mu.Lock()
			cmds = append(cmds, strings.TrimSpace(line))
			mu.Unlock()
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return
			}
			tag := fields[0]
			verb := strings.ToUpper(fields[1])
			switch verb {
			case "LOGIN":
				_, _ = conn.Write([]byte(tag + " OK\r\n"))
			case "EXAMINE":
				_, _ = conn.Write([]byte("* 1 EXISTS\r\n" + tag + " OK [READ-ONLY] examine\r\n"))
			case "SEARCH":
				_, _ = conn.Write([]byte("* SEARCH 1\r\n" + tag + " OK\r\n"))
			case "FETCH":
				header := "From: billing@digitalocean.com\r\nSubject: DigitalOcean invoice\r\nDate: Tue, 01 Sep 2026 12:00:00 +0000\r\n\r\n"
				body := "Amount due: $15.00\r\n"
				_, _ = conn.Write([]byte("* 1 FETCH (BODY[HEADER.FIELDS (FROM SUBJECT DATE)] {" + strconv.Itoa(len(header)) + "}\r\n" + header + " BODY[TEXT] {" + strconv.Itoa(len(body)) + "}\r\n" + body + ")\r\n" + tag + " OK\r\n"))
			case "LOGOUT":
				_, _ = conn.Write([]byte(tag + " OK\r\n"))
				return
			default:
				_, _ = conn.Write([]byte(tag + " BAD\r\n"))
				return
			}
		}
	}()

	password := "s3cret-token"
	src := IMAP{
		Host:     "127.0.0.1",
		Port:     strconv.Itoa(ln.Addr().(*net.TCPAddr).Port),
		User:     "reader",
		Password: password,
		Mailbox:  "INBOX",
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return net.Dial("tcp", ln.Addr().String())
		},
	}
	messages, err := src.Messages(context.Background(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	<-done
	if err != nil {
		if strings.Contains(err.Error(), password) {
			t.Fatal("imap error included the password")
		}
		t.Fatal(err)
	}
	if len(messages) != 1 || !strings.Contains(messages[0].From, "digitalocean.com") || messages[0].Source != "imap" {
		t.Fatalf("messages = %+v", messages)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.ToUpper(strings.Join(cmds, "\n"))
	if strings.Contains(joined, " SELECT ") || strings.Contains(joined, " STORE ") || strings.Contains(joined, " EXPUNGE") {
		t.Fatalf("mailbox was mutated:\n%s", joined)
	}
	if !strings.Contains(joined, " EXAMINE ") || !strings.Contains(joined, "BODY.PEEK") {
		t.Fatalf("commands = %s", joined)
	}
}

func TestIMAPLoginFailureOmitsPassword(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		_, _ = conn.Write([]byte("* OK\r\n"))
		line, _ := reader.ReadString('\n')
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return
		}
		_, _ = conn.Write([]byte(fields[0] + " NO denied\r\n"))
	}()
	password := "s3cret-token"
	src := IMAP{
		Host: "127.0.0.1", Port: strconv.Itoa(ln.Addr().(*net.TCPAddr).Port),
		User: "reader", Password: password,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return net.Dial("tcp", ln.Addr().String())
		},
	}
	_, err = src.Messages(context.Background(), time.Now().AddDate(0, 0, -1))
	if err == nil || strings.Contains(err.Error(), password) || !strings.Contains(err.Error(), "login failed") {
		t.Fatalf("err = %v", err)
	}
}
