package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"zetrc-backend/internal/httpx"
	"zetrc-backend/internal/models"
)

// QuizHandler serves quizzes to learners and marks their attempts.
// Staff create and edit quizzes through the generic CRUD model "Quiz".
type QuizHandler struct {
	db *gorm.DB
	ca *CAService
}

func NewQuizHandler(db *gorm.DB, ca *CAService) *QuizHandler {
	return &QuizHandler{db: db, ca: ca}
}

type learnerQuestion struct {
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
	Marks   float64  `json:"marks"`
}

type learnerQuiz struct {
	Name        string            `json:"name"`
	Course      string            `json:"course"`
	Module      string            `json:"module"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Questions   []learnerQuestion `json:"questions"`
	MaxScore    float64           `json:"max_score"`
	MaxAttempts int               `json:"max_attempts"`
	Attempts    int               `json:"attempts"`
	BestScore   *float64          `json:"best_score"`
}

func questionMarks(q models.QuizQuestion) float64 {
	if q.Marks > 0 {
		return q.Marks
	}
	return 1
}

func (h *QuizHandler) enrolledCourses(email string) []string {
	var courses []string
	h.db.Model(&models.StudentEnrollment{}).
		Where("LOWER(email) = ? AND (status IS NULL OR status <> 'rejected')", email).
		Distinct("course").Pluck("course", &courses)
	return courses
}

// GET /portal/quizzes — published quizzes for the learner's courses (no answers).
func (h *QuizHandler) List(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}
	courses := h.enrolledCourses(email)
	result := []learnerQuiz{}
	if len(courses) == 0 {
		httpx.WriteData(w, http.StatusOK, result)
		return
	}

	var quizzes []models.Quiz
	h.db.Where("status = 'published' AND course IN ?", courses).Order("created_at").Find(&quizzes)

	for _, quiz := range quizzes {
		item := learnerQuiz{
			Name: quiz.Name, Course: quiz.Course, Module: quiz.Module, Title: quiz.Title,
			Description: quiz.Description, MaxAttempts: quiz.MaxAttempts,
		}
		for _, q := range quiz.Questions {
			item.Questions = append(item.Questions, learnerQuestion{Prompt: q.Prompt, Options: q.Options, Marks: questionMarks(q)})
			item.MaxScore += questionMarks(q)
		}
		var attempts []models.QuizAttempt
		h.db.Where("quiz = ? AND LOWER(learner_email) = ?", quiz.Name, email).Find(&attempts)
		item.Attempts = len(attempts)
		for _, a := range attempts {
			score := a.Score
			if item.BestScore == nil || score > *item.BestScore {
				item.BestScore = &score
			}
		}
		result = append(result, item)
	}
	httpx.WriteData(w, http.StatusOK, result)
}

type questionResult struct {
	Correct       bool `json:"correct"`
	Chosen        int  `json:"chosen"`
	CorrectAnswer int  `json:"correct_answer"`
}

// POST /portal/quizzes/{name}/attempt — body {"answers": [optionIndex, …]}.
func (h *QuizHandler) Attempt(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}

	var quiz models.Quiz
	if h.db.Where("name = ? AND status = 'published'", chi.URLParam(r, "name")).First(&quiz).Error != nil {
		httpx.WriteError(w, http.StatusNotFound, "Quiz not found.")
		return
	}

	enrolled := false
	for _, c := range h.enrolledCourses(email) {
		if c == quiz.Course {
			enrolled = true
		}
	}
	if !enrolled {
		httpx.WriteError(w, http.StatusForbidden, "You're not enrolled in "+quiz.Course+".")
		return
	}

	if quiz.MaxAttempts > 0 {
		var used int64
		h.db.Model(&models.QuizAttempt{}).Where("quiz = ? AND LOWER(learner_email) = ?", quiz.Name, email).Count(&used)
		if used >= int64(quiz.MaxAttempts) {
			httpx.WriteError(w, http.StatusForbidden, "You've used all your attempts for this quiz.")
			return
		}
	}

	var req struct {
		Answers []int `json:"answers"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if len(req.Answers) != len(quiz.Questions) {
		httpx.WriteError(w, http.StatusBadRequest, "Please answer every question before submitting.")
		return
	}

	var score, max float64
	results := make([]questionResult, len(quiz.Questions))
	for i, q := range quiz.Questions {
		marks := questionMarks(q)
		max += marks
		correct := req.Answers[i] == q.Answer
		if correct {
			score += marks
		}
		results[i] = questionResult{Correct: correct, Chosen: req.Answers[i], CorrectAnswer: q.Answer}
	}

	attempt := models.QuizAttempt{
		Quiz: quiz.Name, QuizTitle: quiz.Title, Course: quiz.Course,
		LearnerEmail: email, Answers: req.Answers, Score: score, MaxScore: max,
	}
	if err := h.db.Create(&attempt).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not save your attempt.")
		return
	}
	h.ca.EvaluateCertificate(email, quiz.Course)

	httpx.WriteData(w, http.StatusCreated, map[string]any{
		"score":     score,
		"max_score": max,
		"percent":   score / max * 100,
		"results":   results,
		"ca":        h.ca.ForCourse(email, quiz.Course),
	})
}

// quizTitle is a small helper used by other handlers.
func quizTitle(db *gorm.DB, name string) string {
	var quiz models.Quiz
	if db.Select("title").Where("name = ?", name).First(&quiz).Error == nil {
		return strings.TrimSpace(quiz.Title)
	}
	return name
}
