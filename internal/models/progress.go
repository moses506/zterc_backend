package models

import "time"

// LessonProgress is a learner's current progress on one lesson resource
// (today: a lesson's audio). One row per learner + key, keeping the best
// percent reached.
type LessonProgress struct {
	ID           uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	LearnerEmail string    `json:"learner_email" gorm:"size:190;uniqueIndex:idx_progress_learner_key"`
	Key          string    `json:"key" gorm:"size:600;uniqueIndex:idx_progress_learner_key"`
	Module       string    `json:"module"`
	Course       string    `json:"course" gorm:"index"`
	Percent      float64   `json:"percent"`
	Completed    bool      `json:"completed"`
	CreatedAt    time.Time `json:"creation" gorm:"autoCreateTime"`
	UpdatedAt    time.Time `json:"modified" gorm:"autoUpdateTime"`
}

func (LessonProgress) TableName() string { return "lesson_progress" }

// ProgressLog is an append-only history of progress milestones, used to
// draw progress over time.
type ProgressLog struct {
	ID           uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	LearnerEmail string    `json:"learner_email" gorm:"size:190;index"`
	Key          string    `json:"key" gorm:"size:600"`
	Course       string    `json:"course" gorm:"index"`
	Percent      float64   `json:"percent"`
	CreatedAt    time.Time `json:"creation" gorm:"autoCreateTime;index"`
}

func (ProgressLog) TableName() string { return "progress_logs" }
