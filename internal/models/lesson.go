package models

// KeyPoint is a single bullet inside a LessonContent.KeyPoints list.
type KeyPoint struct {
	Title string `json:"title"`
}

// LessonContent maps to the "Lesson_Content" model.
type LessonContent struct {
	Base
	Module        string              `json:"module"`
	Introduction  string              `json:"introduction"`
	Summary       string              `json:"summary"`
	PracticalInfo string              `json:"practical_info"`
	Attachments   string              `json:"attachments"`
	AudioFiles    string              `json:"audio_files"`
	KeyPoints     JSONField[KeyPoint] `json:"key_points" gorm:"type:jsonb"`
}

func (LessonContent) TableName() string { return "lesson_contents" }
