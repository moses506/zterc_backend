package models

import "time"

// Learner is the portal user account. It is intentionally kept outside the
// generic /api/* model registry — learner auth is handled by dedicated
// /portal/* endpoints, matching the original system's separation.
type Learner struct {
	ID            uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	FirstName     string    `json:"first_name"`
	LastName      string    `json:"last_name"`
	Email         string    `json:"email" gorm:"uniqueIndex;size:190"`
	ContactNo     string    `json:"contact_no"`
	PasswordHash  string    `json:"-"`
	EmailVerified bool      `json:"email_verified" gorm:"default:false"`
	Role          string    `json:"role" gorm:"default:Learner"` // Admin | Lecturer | Learner
	Active        bool      `json:"active" gorm:"default:true"`
	Gender        string    `json:"gender"`
	District      string    `json:"district"`
	Cooperative   string    `json:"cooperative"`
	SMSOptIn      bool      `json:"sms_opt_in" gorm:"default:true"`
	CreatedAt     time.Time `json:"creation" gorm:"autoCreateTime"`
	UpdatedAt     time.Time `json:"modified" gorm:"autoUpdateTime"`
}

func (Learner) TableName() string { return "learners" }

// Account roles.
const (
	RoleAdmin    = "Admin"
	RoleLecturer = "Lecturer"
	RoleLearner  = "Learner"
)

// IsValidRole reports whether role is one of the known account roles.
func IsValidRole(role string) bool {
	return role == RoleAdmin || role == RoleLecturer || role == RoleLearner
}

// FullName is a convenience accessor used when shaping API responses.
func (l Learner) FullName() string {
	name := l.FirstName
	if l.LastName != "" {
		if name != "" {
			name += " "
		}
		name += l.LastName
	}
	return name
}
