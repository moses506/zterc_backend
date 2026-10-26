package models

// Document describes one file uploaded alongside a record, e.g. an
// enrollment's supporting documents (ID copy, certificate, photo).
type Document struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// StudentEnrollment maps to the "Student_Enrollment" model.
type StudentEnrollment struct {
	Base
	FirstName   string              `json:"first_name"`
	LastName    string              `json:"last_name"`
	Email       string              `json:"email" gorm:"index"`
	Course      string              `json:"course"`
	Duration    string              `json:"duration"`
	Description string              `json:"description"`
	Documents   JSONField[Document] `json:"documents" gorm:"type:jsonb"`
	// Admin review: pending | approved | rejected. Older rows default to approved.
	Status     string `json:"status" gorm:"default:approved"`
	ReviewNote string `json:"review_note"`
	ReviewedBy string `json:"reviewed_by"`
}

func (StudentEnrollment) TableName() string { return "student_enrollments" }
