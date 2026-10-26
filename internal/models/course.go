package models

// Course maps to the "Course" model used by /api/* CRUD calls.
type Course struct {
	Base
	Duration    string `json:"duration"`
	Description string `json:"description"`
	// Lecturer is the email of the lecturer account that runs this course.
	Lecturer string `json:"lecturer" gorm:"index"`
}

func (Course) TableName() string { return "courses" }

// CourseModule maps to the "Course_Module" model.
type CourseModule struct {
	Base
	Course      string `json:"course"`
	Title       string `json:"title"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

func (CourseModule) TableName() string { return "course_modules" }
