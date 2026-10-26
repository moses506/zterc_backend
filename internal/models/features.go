package models

import "time"

// Notification is a message for one account, shown in the in-app bell and
// optionally delivered by SMS / WhatsApp.
type Notification struct {
	ID             uint       `json:"id" gorm:"primaryKey;autoIncrement"`
	RecipientEmail string     `json:"recipient_email" gorm:"size:190;index"`
	Kind           string     `json:"kind"` // marked | announcement | enrollment | certificate | reply | lesson
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	Link           string     `json:"link"` // app view to open, e.g. "assignments"
	ReadAt         *time.Time `json:"read_at"`
	CreatedAt      time.Time  `json:"creation" gorm:"autoCreateTime;index"`
}

func (Notification) TableName() string { return "notifications" }

// Announcement is posted by a lecturer (to a course) or an admin (to a course
// or, with an empty course, to everyone). Registered as "Announcement".
type Announcement struct {
	Base
	Course      string `json:"course" gorm:"index"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Priority    string `json:"priority"` // info | important
	AuthorName  string `json:"author_name"`
	AuthorEmail string `json:"author_email"`
}

func (Announcement) TableName() string { return "announcements" }

// QuizQuestion is one multiple-choice question. Answer is the index of the
// correct option and is never sent to learners.
type QuizQuestion struct {
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
	Answer  int      `json:"answer"`
	Marks   float64  `json:"marks"`
}

// Quiz is an auto-marked multiple-choice quiz for a course. Registered as "Quiz".
type Quiz struct {
	Base
	Course      string                  `json:"course" gorm:"index"`
	Module      string                  `json:"module"`
	Title       string                  `json:"title"`
	Description string                  `json:"description"`
	Questions   JSONField[QuizQuestion] `json:"questions" gorm:"type:jsonb"`
	Status      string                  `json:"status"` // draft | published
	MaxAttempts int                     `json:"max_attempts"`
}

func (Quiz) TableName() string { return "quizzes" }

// QuizAttempt is one scored attempt by a learner.
type QuizAttempt struct {
	ID           uint           `json:"id" gorm:"primaryKey;autoIncrement"`
	Quiz         string         `json:"quiz" gorm:"size:140;index"`
	QuizTitle    string         `json:"quiz_title"`
	Course       string         `json:"course" gorm:"index"`
	LearnerEmail string         `json:"learner_email" gorm:"size:190;index"`
	Answers      JSONField[int] `json:"answers" gorm:"type:jsonb"`
	Score        float64        `json:"score"`
	MaxScore     float64        `json:"max_score"`
	CreatedAt    time.Time      `json:"creation" gorm:"autoCreateTime"`
}

func (QuizAttempt) TableName() string { return "quiz_attempts" }

// AuditLog records every change made by staff through the admin and lecturer APIs.
type AuditLog struct {
	ID         uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	ActorEmail string    `json:"actor_email" gorm:"size:190;index"`
	ActorRole  string    `json:"actor_role"`
	Action     string    `json:"action"` // create | update | delete | review | login
	Model      string    `json:"model" gorm:"index"`
	Record     string    `json:"record"`
	Summary    string    `json:"summary"`
	Changes    string    `json:"changes" gorm:"type:text"`
	CreatedAt  time.Time `json:"creation" gorm:"autoCreateTime;index"`
}

func (AuditLog) TableName() string { return "audit_logs" }

// DiscussionThread is a question or topic posted in a course discussion.
type DiscussionThread struct {
	ID           uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	Course       string    `json:"course" gorm:"index"`
	Title        string    `json:"title"`
	Body         string    `json:"body" gorm:"type:text"`
	AuthorEmail  string    `json:"author_email"`
	AuthorName   string    `json:"author_name"`
	AuthorRole   string    `json:"author_role"`
	ReplyCount   int       `json:"reply_count"`
	Resolved     bool      `json:"resolved"`
	LastActivity time.Time `json:"last_activity" gorm:"index"`
	CreatedAt    time.Time `json:"creation" gorm:"autoCreateTime"`
}

func (DiscussionThread) TableName() string { return "discussion_threads" }

// DiscussionReply is a reply within a thread.
type DiscussionReply struct {
	ID          uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	ThreadID    uint      `json:"thread_id" gorm:"index"`
	Body        string    `json:"body" gorm:"type:text"`
	AuthorEmail string    `json:"author_email"`
	AuthorName  string    `json:"author_name"`
	AuthorRole  string    `json:"author_role"`
	CreatedAt   time.Time `json:"creation" gorm:"autoCreateTime"`
}

func (DiscussionReply) TableName() string { return "discussion_replies" }

// FieldPhoto is a geotagged photo in a learner's field journal.
type FieldPhoto struct {
	ID           uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	LearnerEmail string    `json:"learner_email" gorm:"size:190;index"`
	LearnerName  string    `json:"learner_name"`
	Course       string    `json:"course" gorm:"index"`
	Caption      string    `json:"caption"`
	ImageURL     string    `json:"image_url"`
	Latitude     *float64  `json:"latitude"`
	Longitude    *float64  `json:"longitude"`
	TakenAt      string    `json:"taken_at"`
	CreatedAt    time.Time `json:"creation" gorm:"autoCreateTime"`
}

func (FieldPhoto) TableName() string { return "field_photos" }

// FeatureModels lists the non-registry tables to migrate.
func FeatureModels() []any {
	return []any{&Notification{}, &QuizAttempt{}, &AuditLog{}, &DiscussionThread{}, &DiscussionReply{}, &FieldPhoto{}}
}
