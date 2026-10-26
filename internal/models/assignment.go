package models

// Assignment maps to the "Assignment" model.
type Assignment struct {
	Base
	Course           string `json:"course"`
	SubmissionDate   string `json:"submission_date"`
	Description      string `json:"description"`
	Remarks          string `json:"remarks"`
	Status           string `json:"status"`
	Owner            string `json:"owner"`
	FirstName        string `json:"first_name"`
	LastName         string `json:"last_name"`
	ContactNo        string `json:"contact_no"`
	LecturerName     string `json:"lecturer_name"`
	ModifiedBy       string `json:"modified_by"`
	StatusColor      string `json:"status_color"`
	StatusInnerColor string `json:"status_inner_color"`
}

func (Assignment) TableName() string { return "assignments" }

// AssignmentSubmission maps to the "Assignment_Submission" model.
type AssignmentSubmission struct {
	Base
	Assignment       string `json:"assignment"`
	AssignmentName   string `json:"assignment_name"`
	StudentFirstName string `json:"student_first_name"`
	StudentLastName  string `json:"student_last_name"`
	Student          string `json:"student"`
	Content          string `json:"content"`
	Attachment       string `json:"attachment"`
	// Marking, filled in by the course lecturer.
	Grade    string  `json:"grade"`     // optional label, e.g. "B+"
	Score    float64 `json:"score"`     // marks awarded
	MaxScore float64 `json:"max_score"` // marks available; 0 = not scored
	Feedback string  `json:"feedback"`
	Status   string  `json:"status"` // submitted | marked | returned
}

func (AssignmentSubmission) TableName() string { return "assignment_submissions" }
