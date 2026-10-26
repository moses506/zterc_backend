package models

import "time"

// OTPPurpose distinguishes why a one-time-passcode was issued.
type OTPPurpose string

const (
	OTPPurposeSignup OTPPurpose = "signup"
	OTPPurposeLogin  OTPPurpose = "login"
	OTPPurposeReset  OTPPurpose = "reset"
)

type OTP struct {
	ID        uint       `json:"id" gorm:"primaryKey;autoIncrement"`
	Email     string     `json:"email" gorm:"index;size:190"`
	Code      string     `json:"-"`
	Purpose   OTPPurpose `json:"purpose"`
	ExpiresAt time.Time  `json:"expires_at"`
	Consumed  bool       `json:"consumed" gorm:"default:false"`
	CreatedAt time.Time  `json:"creation" gorm:"autoCreateTime"`
}

func (OTP) TableName() string { return "otps" }
