package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"zetrc-backend/internal/httpx"
	"zetrc-backend/internal/models"
)

// GradebookHandler returns every learner × every assessment for a course,
// with CA and at-risk flags. Mounted under /admin/api and /lecturer/api.
type GradebookHandler struct {
	db *gorm.DB
	ca *CAService
}

func NewGradebookHandler(db *gorm.DB, ca *CAService) *GradebookHandler {
	return &GradebookHandler{db: db, ca: ca}
}

type gradebookColumn struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	Due   string `json:"due"`
}

type gradebookCell struct {
	Score    float64 `json:"score"`
	MaxScore float64 `json:"max_score"`
	Status   string  `json:"status"`
}

type gradebookRow struct {
	Email      string                   `json:"email"`
	Name       string                   `json:"name"`
	Cells      map[string]gradebookCell `json:"cells"`
	CA         *float64                 `json:"ca"`
	Assessed   int                      `json:"assessed"`
	LastActive string                   `json:"last_active"`
	Risks      []string                 `json:"risks"`
}

// canAccessCourse: admins see all courses, lecturers only their own.
func canAccessCourse(db *gorm.DB, r *http.Request, course string) bool {
	claims := ClaimsFrom(r)
	if claims == nil {
		return false
	}
	switch claims.Role {
	case models.RoleAdmin:
		return true
	case models.RoleLecturer:
		var count int64
		db.Model(&models.Course{}).Where("name = ? AND LOWER(lecturer) = ?", course, strings.ToLower(claims.Email)).Count(&count)
		return count > 0
	}
	return false
}

const inactiveDays = 10

// GET /…/gradebook?course=X
func (h *GradebookHandler) Get(w http.ResponseWriter, r *http.Request) {
	course := strings.TrimSpace(r.URL.Query().Get("course"))
	if course == "" {
		httpx.WriteError(w, http.StatusBadRequest, "Choose a course.")
		return
	}
	if !canAccessCourse(h.db, r, course) {
		httpx.WriteError(w, http.StatusForbidden, "You don't have access to this course.")
		return
	}

	var assignments []models.Assignment
	h.db.Where("course = ?", course).Order("submission_date, created_at").Find(&assignments)
	var quizzes []models.Quiz
	h.db.Where("course = ? AND status = 'published'", course).Order("created_at").Find(&quizzes)

	columns := []gradebookColumn{}
	for _, a := range assignments {
		columns = append(columns, gradebookColumn{Key: a.Name, Title: a.Name, Kind: "assignment", Due: a.SubmissionDate})
	}
	for _, q := range quizzes {
		columns = append(columns, gradebookColumn{Key: q.Name, Title: q.Title, Kind: "quiz"})
	}

	var enrollments []models.StudentEnrollment
	h.db.Where("course = ? AND (status IS NULL OR status <> 'rejected')", course).Order("created_at").Find(&enrollments)

	today := time.Now().Format("2006-01-02")
	rows := []gradebookRow{}
	seen := map[string]bool{}
	for _, e := range enrollments {
		email := strings.ToLower(e.Email)
		if seen[email] {
			continue
		}
		seen[email] = true

		ca := h.ca.ForCourse(email, course)
		row := gradebookRow{
			Email: email, Name: strings.TrimSpace(e.FirstName + " " + e.LastName),
			Cells: map[string]gradebookCell{}, CA: ca.Percent, Assessed: ca.Assessed, Risks: []string{},
		}
		for _, item := range ca.Items {
			row.Cells[item.Key] = gradebookCell{Score: item.Score, MaxScore: item.MaxScore, Status: item.Status}
		}

		// Last activity across enrollment, submissions, quiz attempts and lesson progress.
		last := e.CreatedAt
		var t time.Time
		if h.db.Model(&models.AssignmentSubmission{}).Where("LOWER(student) = ?", email).Select("MAX(created_at)").Scan(&t); t.After(last) {
			last = t
		}
		t = time.Time{}
		if h.db.Model(&models.QuizAttempt{}).Where("LOWER(learner_email) = ?", email).Select("MAX(created_at)").Scan(&t); t.After(last) {
			last = t
		}
		t = time.Time{}
		if h.db.Model(&models.LessonProgress{}).Where("learner_email = ?", email).Select("MAX(updated_at)").Scan(&t); t.After(last) {
			last = t
		}
		row.LastActive = last.Format(time.RFC3339)

		if ca.Percent != nil && *ca.Percent < h.ca.PassMark {
			row.Risks = append(row.Risks, fmt.Sprintf("CA %.0f%% (below %.0f%%)", *ca.Percent, h.ca.PassMark))
		}
		if days := int(time.Since(last).Hours() / 24); days >= inactiveDays {
			row.Risks = append(row.Risks, fmt.Sprintf("No activity for %d days", days))
		}
		overdue := 0
		for _, a := range assignments {
			if a.SubmissionDate != "" && a.SubmissionDate < today {
				if _, submitted := row.Cells[a.Name]; !submitted {
					overdue++
				}
			}
		}
		if overdue > 0 {
			row.Risks = append(row.Risks, fmt.Sprintf("%d overdue assignment(s)", overdue))
		}
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(i, j int) bool { return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name) })

	httpx.WriteData(w, http.StatusOK, map[string]any{
		"course":    course,
		"pass_mark": h.ca.PassMark,
		"columns":   columns,
		"rows":      rows,
	})
}
