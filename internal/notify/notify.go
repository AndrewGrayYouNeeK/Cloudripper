package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Message is a human-readable alert.
type Message struct {
	Subject string
	Body    string
}

// Notifier delivers a message.
type Notifier interface {
	Name() string
	Notify(ctx context.Context, msg Message) error
}

// Fanout delivers to every notifier. The first error is returned after all attempts.
func Fanout(ctx context.Context, notifiers []Notifier, msg Message) error {
	var errs []string
	for _, n := range notifiers {
		if n == nil {
			continue
		}
		if err := n.Notify(ctx, msg); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", n.Name(), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// Slack posts text to an incoming webhook.
type Slack struct {
	Webhook string
	Client  *http.Client
}

// Name implements Notifier.
func (s Slack) Name() string { return "slack" }

// Notify implements Notifier.
func (s Slack) Notify(ctx context.Context, msg Message) error {
	if strings.TrimSpace(s.Webhook) == "" {
		return fmt.Errorf("slack webhook is empty")
	}
	payload, err := json.Marshal(map[string]string{
		"text": strings.TrimSpace(msg.Subject + "\n" + msg.Body),
	})
	if err != nil {
		return err
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Webhook, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// Email sends via SMTP. Empty credentials skip authentication.
type Email struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       string
}

// Name implements Notifier.
func (e Email) Name() string { return "email" }

// Notify implements Notifier.
func (e Email) Notify(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.Host == "" || e.To == "" || e.From == "" {
		return fmt.Errorf("email requires host, from, and to")
	}
	port := e.Port
	if port == 0 {
		port = 587
	}
	addr := e.Host + ":" + strconv.Itoa(port)
	var auth smtp.Auth
	if e.Username != "" {
		auth = smtp.PlainAuth("", e.Username, e.Password, e.Host)
	}
	body := "To: " + e.To + "\r\n" +
		"From: " + e.From + "\r\n" +
		"Subject: " + sanitizeHeader(msg.Subject) + "\r\n" +
		"\r\n" + msg.Body + "\r\n"
	return smtp.SendMail(addr, auth, e.From, []string{e.To}, []byte(body))
}

func sanitizeHeader(v string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(v)
}

// Stdout writes alerts to a writer. It is the fallback when no webhook or SMTP is set.
type Stdout struct {
	Writer io.Writer
}

// Name implements Notifier.
func (s Stdout) Name() string { return "stdout" }

// Notify implements Notifier.
func (s Stdout) Notify(_ context.Context, msg Message) error {
	if s.Writer == nil {
		return fmt.Errorf("stdout writer is nil")
	}
	_, err := fmt.Fprintf(s.Writer, "%s\n%s\n", msg.Subject, msg.Body)
	return err
}
