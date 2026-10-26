// Package notify stores in-app notifications and delivers them by SMS and/or
// WhatsApp through a pluggable provider.
//
// Providers are chosen with NOTIFY_CHANNELS (comma-separated):
//
//	log          – print messages to the server log (default, for development)
//	africastalking – SMS via Africa's Talking (AT_USERNAME, AT_API_KEY, AT_SENDER_ID)
//	whatsapp     – WhatsApp Cloud API (WHATSAPP_PHONE_NUMBER_ID, WHATSAPP_TOKEN)
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"

	"zetrc-backend/internal/models"
)

// Sender delivers a text message to a phone number.
type Sender interface {
	Name() string
	Send(phone, text string) error
}

// Service saves notifications and fans them out to the configured senders.
type Service struct {
	db      *gorm.DB
	senders []Sender
}

// New builds the service from environment variables.
func New(db *gorm.DB) *Service {
	channels := os.Getenv("NOTIFY_CHANNELS")
	if strings.TrimSpace(channels) == "" {
		channels = "log"
	}

	var senders []Sender
	for _, name := range strings.Split(channels, ",") {
		switch strings.TrimSpace(strings.ToLower(name)) {
		case "log":
			senders = append(senders, logSender{})
		case "africastalking", "sms":
			if s, ok := newAfricasTalking(); ok {
				senders = append(senders, s)
			} else {
				log.Printf("notify: africastalking selected but AT_USERNAME / AT_API_KEY are missing — skipping")
			}
		case "whatsapp":
			if s, ok := newWhatsApp(); ok {
				senders = append(senders, s)
			} else {
				log.Printf("notify: whatsapp selected but WHATSAPP_PHONE_NUMBER_ID / WHATSAPP_TOKEN are missing — skipping")
			}
		case "":
		default:
			log.Printf("notify: unknown channel %q ignored", name)
		}
	}

	names := make([]string, 0, len(senders))
	for _, s := range senders {
		names = append(names, s.Name())
	}
	log.Printf("notify: delivery channels: %s", strings.Join(names, ", "))
	return &Service{db: db, senders: senders}
}

// Message is one notification.
type Message struct {
	Kind  string
	Title string
	Body  string
	Link  string
	// SMS sends the message by SMS/WhatsApp too (in-app is always saved).
	SMS bool
}

// Notify saves the notification for each recipient and, when msg.SMS is set,
// sends it to recipients who have a phone number and have not opted out.
func (s *Service) Notify(emails []string, msg Message) {
	if s == nil {
		return
	}
	seen := map[string]bool{}
	for _, raw := range emails {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true

		s.db.Create(&models.Notification{
			RecipientEmail: email,
			Kind:           msg.Kind,
			Title:          msg.Title,
			Body:           msg.Body,
			Link:           msg.Link,
		})

		if !msg.SMS || len(s.senders) == 0 {
			continue
		}
		var account models.Learner
		if s.db.Where("LOWER(email) = ?", email).First(&account).Error != nil || !account.SMSOptIn {
			continue
		}
		phone := NormalizeZambianPhone(account.ContactNo)
		if phone == "" {
			continue
		}
		text := smsText(msg)
		for _, sender := range s.senders {
			go func(sender Sender) {
				if err := sender.Send(phone, text); err != nil {
					log.Printf("notify: %s to %s failed: %v", sender.Name(), phone, err)
				}
			}(sender)
		}
	}
}

func smsText(msg Message) string {
	text := "ZETRC: " + msg.Title
	if msg.Body != "" {
		text += " - " + msg.Body
	}
	// Keep to plain characters so messages use the cheaper GSM-7 SMS encoding.
	text = strings.NewReplacer("—", "-", "–", "-", "…", "...", "“", "\"", "”", "\"", "’", "'", "‘", "'").Replace(text)
	if len(text) > 300 {
		text = text[:297] + "..."
	}
	return text
}

// NormalizeZambianPhone turns 0971234567 / 260971234567 / +260 97 123 4567
// into +260971234567. Returns "" when it does not look like a phone number.
func NormalizeZambianPhone(raw string) string {
	var digits strings.Builder
	for _, r := range raw {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	switch {
	case len(d) == 10 && strings.HasPrefix(d, "0"):
		return "+260" + d[1:]
	case len(d) == 9 && (strings.HasPrefix(d, "9") || strings.HasPrefix(d, "7")):
		return "+260" + d
	case len(d) == 12 && strings.HasPrefix(d, "260"):
		return "+" + d
	case len(d) >= 10 && strings.HasPrefix(raw, "+"):
		return "+" + d
	}
	return ""
}

// ---- providers ----

type logSender struct{}

func (logSender) Name() string { return "log" }
func (logSender) Send(phone, text string) error {
	log.Printf("[notify:log] to %s: %s", phone, text)
	return nil
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

type africasTalking struct {
	username, apiKey, senderID, endpoint string
}

func newAfricasTalking() (Sender, bool) {
	username, apiKey := os.Getenv("AT_USERNAME"), os.Getenv("AT_API_KEY")
	if username == "" || apiKey == "" {
		return nil, false
	}
	endpoint := "https://api.africastalking.com/version1/messaging"
	if username == "sandbox" {
		endpoint = "https://api.sandbox.africastalking.com/version1/messaging"
	}
	return africasTalking{username, apiKey, os.Getenv("AT_SENDER_ID"), endpoint}, true
}

func (a africasTalking) Name() string { return "africastalking" }

func (a africasTalking) Send(phone, text string) error {
	form := url.Values{"username": {a.username}, "to": {phone}, "message": {text}}
	if a.senderID != "" {
		form.Set("from", a.senderID)
	}
	req, err := http.NewRequest(http.MethodPost, a.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("apiKey", a.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return doRequest(req)
}

type whatsApp struct {
	phoneNumberID, token string
}

func newWhatsApp() (Sender, bool) {
	id, token := os.Getenv("WHATSAPP_PHONE_NUMBER_ID"), os.Getenv("WHATSAPP_TOKEN")
	if id == "" || token == "" {
		return nil, false
	}
	return whatsApp{id, token}, true
}

func (w whatsApp) Name() string { return "whatsapp" }

func (w whatsApp) Send(phone, text string) error {
	body, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                strings.TrimPrefix(phone, "+"),
		"type":              "text",
		"text":              map[string]string{"body": text},
	})
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("https://graph.facebook.com/v20.0/%s/messages", w.phoneNumberID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("Content-Type", "application/json")
	return doRequest(req)
}

func doRequest(req *http.Request) error {
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	return nil
}
