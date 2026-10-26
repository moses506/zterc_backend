package otp

import (
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"time"

	"gorm.io/gorm"

	"zetrc-backend/internal/models"
)

type Service struct {
	db  *gorm.DB
	ttl time.Duration
}

func NewService(db *gorm.DB, ttl time.Duration) *Service {
	return &Service{db: db, ttl: ttl}
}

// Generate creates and stores a fresh 6-digit OTP for email/purpose. In this
// build the code is only logged to stdout — swap Deliver() for a real
// email/SMS provider (e.g. SMTP or Africa's Talking) before going to
// production.
func (s *Service) Generate(email string, purpose models.OTPPurpose) (string, error) {
	code, err := randomCode()
	if err != nil {
		return "", err
	}

	record := models.OTP{
		Email:     email,
		Code:      code,
		Purpose:   purpose,
		ExpiresAt: time.Now().Add(s.ttl),
	}
	if err := s.db.Create(&record).Error; err != nil {
		return "", err
	}

	s.Deliver(email, code)
	return code, nil
}

// Deliver "sends" the OTP. Replace this with a real email/SMS integration.
func (s *Service) Deliver(email, code string) {
	log.Printf("[otp] code %s issued for %s (dev stub: no email/SMS provider configured)", code, email)
}

// Verify checks code against the most recent unconsumed, unexpired OTP for
// email+purpose, consuming it on success.
func (s *Service) Verify(email, code string, purpose models.OTPPurpose) (bool, error) {
	var record models.OTP
	err := s.db.Where("email = ? AND purpose = ? AND consumed = ? AND expires_at > ?", email, purpose, false, time.Now()).
		Order("created_at DESC").
		First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, err
	}

	if record.Code != code {
		return false, nil
	}

	record.Consumed = true
	if err := s.db.Save(&record).Error; err != nil {
		return false, err
	}
	return true, nil
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
