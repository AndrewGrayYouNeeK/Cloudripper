package bills

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

// IMAP reads billing mail over TLS. It opens the mailbox with EXAMINE and
// fetches BODY.PEEK, so it does not mark messages read, delete them, or change flags.
type IMAP struct {
	Host     string
	Port     string
	User     string
	Password string
	Mailbox  string
	Dial     func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Messages implements a read-only mailbox walk since the given time.
func (s IMAP) Messages(ctx context.Context, since time.Time) ([]Message, error) {
	if strings.TrimSpace(s.Host) == "" || strings.TrimSpace(s.User) == "" {
		return nil, fmt.Errorf("imap host and user are required")
	}
	if s.Password == "" {
		return nil, fmt.Errorf("imap password is not set")
	}
	if s.Mailbox == "" {
		s.Mailbox = "INBOX"
	}
	if s.Port == "" {
		s.Port = "993"
	}
	if since.IsZero() {
		since = time.Now().AddDate(0, 0, -120)
	}
	conn, err := s.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("imap dial: %s", redact(s.Password, err.Error()))
	}
	defer conn.Close()
	client := &imapConn{conn: conn, r: bufio.NewReader(conn)}
	if err := client.greeting(); err != nil {
		return nil, redactErr(s.Password, err)
	}
	if _, err := client.roundTrip("LOGIN " + quoteIMAP(s.User) + " " + quoteIMAP(s.Password)); err != nil {
		return nil, fmt.Errorf("imap login failed")
	}
	if _, err := client.roundTrip("EXAMINE " + quoteIMAP(s.Mailbox)); err != nil {
		return nil, redactErr(s.Password, fmt.Errorf("imap examine: %w", err))
	}
	rawSearch, err := client.roundTrip("SEARCH SINCE " + since.UTC().Format("02-Jan-2006"))
	if err != nil {
		return nil, redactErr(s.Password, fmt.Errorf("imap search: %w", err))
	}
	ids := newestIDs(parseSearch(rawSearch), 200)
	var messages []Message
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return messages, err
		}
		raw, err := client.roundTrip("FETCH " + id + " (BODY.PEEK[HEADER.FIELDS (FROM SUBJECT DATE)] BODY.PEEK[TEXT])")
		if err != nil {
			return messages, redactErr(s.Password, fmt.Errorf("imap fetch: %w", err))
		}
		msg, ok := messageFromFetch(raw, "imap")
		if ok {
			messages = append(messages, msg)
		}
	}
	_, _ = client.roundTrip("LOGOUT")
	return messages, nil
}

func (s IMAP) dial(ctx context.Context) (net.Conn, error) {
	addr := net.JoinHostPort(s.Host, s.Port)
	if s.Dial != nil {
		return s.Dial(ctx, "tcp", addr)
	}
	dialer := tls.Dialer{Config: &tls.Config{ServerName: s.Host}}
	return dialer.DialContext(ctx, "tcp", addr)
}

type imapConn struct {
	conn net.Conn
	r    *bufio.Reader
	seq  int
}

func (c *imapConn) greeting() error {
	line, err := c.readLine()
	if err != nil {
		return err
	}
	if !strings.Contains(strings.ToUpper(line), "OK") {
		return fmt.Errorf("unexpected imap greeting")
	}
	return nil
}

func (c *imapConn) roundTrip(command string) (string, error) {
	c.seq++
	tag := fmt.Sprintf("a%03d", c.seq)
	if _, err := fmt.Fprintf(c.conn, "%s %s\r\n", tag, command); err != nil {
		return "", err
	}
	var b strings.Builder
	for {
		line, err := c.readLine()
		if err != nil {
			return b.String(), err
		}
		b.WriteString(line)
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, tag+" ") {
			upper := strings.ToUpper(trimmed)
			if strings.Contains(upper, " OK") {
				return b.String(), nil
			}
			return b.String(), fmt.Errorf("%s", strings.TrimSpace(trimmed))
		}
	}
}

func (c *imapConn) readLine() (string, error) {
	var buf strings.Builder
	for {
		part, err := c.r.ReadString('\n')
		buf.WriteString(part)
		if err != nil {
			return buf.String(), err
		}
		n, ok := literalSize(strings.TrimRight(part, "\r\n"))
		if !ok {
			return buf.String(), nil
		}
		literal := make([]byte, n)
		if _, err := io.ReadFull(c.r, literal); err != nil {
			return "", err
		}
		buf.Write(literal)
	}
}

func literalSize(line string) (int, bool) {
	i := strings.LastIndex(line, "{")
	if i < 0 || !strings.HasSuffix(line, "}") {
		return 0, false
	}
	n, err := strconv.Atoi(line[i+1 : len(line)-1])
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func quoteIMAP(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func redact(secret, msg string) string {
	if secret == "" {
		return msg
	}
	return strings.ReplaceAll(msg, secret, "[redacted]")
}

func redactErr(secret string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", redact(secret, err.Error()))
}

func parseSearch(raw string) []string {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && strings.EqualFold(fields[0], "*") && strings.EqualFold(fields[1], "SEARCH") {
			if len(fields) == 2 {
				return nil
			}
			return fields[2:]
		}
	}
	return nil
}

func newestIDs(ids []string, limit int) []string {
	sort.Slice(ids, func(i, j int) bool {
		ai, _ := strconv.Atoi(ids[i])
		aj, _ := strconv.Atoi(ids[j])
		return ai < aj
	})
	if limit > 0 && len(ids) > limit {
		ids = ids[len(ids)-limit:]
	}
	return ids
}

func messageFromFetch(raw, source string) (Message, bool) {
	from := headerField(raw, "From")
	subject := headerField(raw, "Subject")
	if from == "" && subject == "" {
		return Message{}, false
	}
	when, _ := mailParseDate(headerField(raw, "Date"))
	return Message{
		From:    from,
		Subject: subject,
		Date:    when,
		Body:    bodyFromFetch(raw),
		Source:  source,
	}, true
}

func headerField(raw, name string) string {
	prefix := strings.ToLower(name) + ":"
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(strings.ToLower(line), prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}

func bodyFromFetch(raw string) string {
	if i := strings.Index(raw, "\r\n\r\n"); i >= 0 {
		return raw[i+4:]
	}
	if i := strings.Index(raw, "\n\n"); i >= 0 {
		return raw[i+2:]
	}
	return raw
}
