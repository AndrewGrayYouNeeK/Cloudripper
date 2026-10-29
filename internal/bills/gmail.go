package bills

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"
)

// Gmail reads messages with the Gmail API. It only sends GET requests.
type Gmail struct {
	TokenPath string
	BaseURL   string
	Client    *http.Client
	Since     time.Time
}

type gmailToken struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	Expiry      time.Time `json:"expiry"`
}

type gmailList struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
	NextPageToken string `json:"nextPageToken"`
}

type gmailPart struct {
	MimeType string `json:"mimeType"`
	Body     struct {
		Data string `json:"data"`
	} `json:"body"`
	Parts   []gmailPart `json:"parts"`
	Headers []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"headers"`
}

type gmailFull struct {
	ID      string    `json:"id"`
	Payload gmailPart `json:"payload"`
}

// Messages lists recent billing mail and downloads each message once.
func (g Gmail) Messages(ctx context.Context) ([]Message, error) {
	if strings.TrimSpace(g.TokenPath) == "" {
		return nil, fmt.Errorf("gmail token file is not set")
	}
	token, err := readToken(g.TokenPath)
	if err != nil {
		return nil, err
	}
	if g.BaseURL == "" {
		g.BaseURL = "https://gmail.googleapis.com"
	}
	if g.Client == nil {
		g.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if g.Since.IsZero() {
		g.Since = time.Now().AddDate(0, 0, -120)
	}
	ids, err := g.listIDs(ctx, token.AccessToken)
	if err != nil {
		return nil, err
	}
	var messages []Message
	for _, id := range ids {
		msg, err := g.fetch(ctx, token.AccessToken, id)
		if err != nil {
			return messages, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func readToken(path string) (gmailToken, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return gmailToken{}, fmt.Errorf("gmail token file: %w", err)
	}
	var token gmailToken
	if err := json.Unmarshal(data, &token); err != nil {
		return gmailToken{}, fmt.Errorf("gmail token file is not valid json")
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return gmailToken{}, fmt.Errorf("gmail token file has no access_token")
	}
	if !token.Expiry.IsZero() && time.Now().After(token.Expiry) {
		return gmailToken{}, fmt.Errorf("gmail access token is expired")
	}
	return token, nil
}

func (g Gmail) listIDs(ctx context.Context, access string) ([]string, error) {
	var ids []string
	page := ""
	for len(ids) < 100 {
		query := url.Values{}
		query.Set("q", gmailQuery(g.Since))
		query.Set("maxResults", "50")
		if page != "" {
			query.Set("pageToken", page)
		}
		var listed gmailList
		if err := g.getJSON(ctx, access, "/gmail/v1/users/me/messages?"+query.Encode(), &listed); err != nil {
			return nil, err
		}
		for _, msg := range listed.Messages {
			if msg.ID == "" {
				continue
			}
			ids = append(ids, msg.ID)
			if len(ids) >= 100 {
				break
			}
		}
		if listed.NextPageToken == "" || len(listed.Messages) == 0 {
			break
		}
		page = listed.NextPageToken
	}
	return ids, nil
}

func gmailQuery(since time.Time) string {
	var domains []string
	for _, provider := range Catalog() {
		for _, domain := range provider.Domains {
			domains = append(domains, "from:"+domain)
		}
	}
	return fmt.Sprintf("after:%s (%s OR subject:invoice OR subject:billing OR subject:receipt)",
		since.UTC().Format("2006/01/02"), strings.Join(domains, " OR "))
}

func (g Gmail) fetch(ctx context.Context, access, id string) (Message, error) {
	var full gmailFull
	path := "/gmail/v1/users/me/messages/" + url.PathEscape(id) + "?format=full"
	if err := g.getJSON(ctx, access, path, &full); err != nil {
		return Message{}, err
	}
	msg := Message{
		From:    headerValue(full.Payload, "From"),
		Subject: headerValue(full.Payload, "Subject"),
		Body:    partText(full.Payload),
		Source:  "gmail",
	}
	if when, err := mailParseDate(headerValue(full.Payload, "Date")); err == nil {
		msg.Date = when
	}
	return msg, nil
}

func (g Gmail) getJSON(ctx context.Context, access, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(g.BaseURL, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := g.Client.Do(req)
	if err != nil {
		return fmt.Errorf("gmail request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("gmail response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("gmail status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("gmail response was not json")
	}
	return nil
}

func headerValue(part gmailPart, name string) string {
	for _, header := range part.Headers {
		if strings.EqualFold(header.Name, name) {
			return header.Value
		}
	}
	return ""
}

func partText(part gmailPart) string {
	if strings.HasPrefix(strings.ToLower(part.MimeType), "text/plain") && part.Body.Data != "" {
		if text, err := decodeGmail(part.Body.Data); err == nil {
			return text
		}
	}
	var texts []string
	for _, child := range part.Parts {
		if text := partText(child); text != "" {
			texts = append(texts, text)
		}
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n")
	}
	if part.Body.Data == "" {
		return ""
	}
	text, err := decodeGmail(part.Body.Data)
	if err != nil {
		return ""
	}
	return text
}

func decodeGmail(data string) (string, error) {
	data = strings.TrimSpace(data)
	if decoded, err := base64.RawURLEncoding.DecodeString(data); err == nil {
		return string(decoded), nil
	}
	decoded, err := base64.URLEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func mailParseDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	return mail.ParseDate(value)
}
