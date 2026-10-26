package models

// Certificate maps to the "Certificate" model — one record per
// learner+course, tracking completion status of a training credential.
type Certificate struct {
	Base
	LearnerEmail string `json:"learner_email" gorm:"index"`
	Course       string `json:"course"`
	Status       string `json:"status"` // earned | in_review | locked
	IssuedDate   string `json:"issued_date"`
	Detail       string `json:"detail"`
	// Public verification code printed on the certificate (QR + URL).
	Code      string  `json:"code" gorm:"size:40;index"`
	CAPercent float64 `json:"ca_percent"`
}

func (Certificate) TableName() string { return "certificates" }
