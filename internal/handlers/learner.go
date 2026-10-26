package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"zetrc-backend/internal/httpx"
	"zetrc-backend/internal/models"
)

// LearnerHandler serves data that belongs to the signed-in account.
type LearnerHandler struct {
	db *gorm.DB
	ca *CAService
}

func NewLearnerHandler(db *gorm.DB, ca *CAService) *LearnerHandler {
	return &LearnerHandler{db: db, ca: ca}
}

func signedInEmail(w http.ResponseWriter, r *http.Request) (string, bool) {
	claims := ClaimsFrom(r)
	if claims == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "Please sign in again.")
		return "", false
	}
	return strings.ToLower(claims.Email), true
}

type mySubmission struct {
	models.AssignmentSubmission
	Course string `json:"course"`
}

// GET /portal/my-submissions
func (h *LearnerHandler) MySubmissions(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}

	var submissions []models.AssignmentSubmission
	if err := h.db.Where("LOWER(student) = ?", email).Order("updated_at DESC").Find(&submissions).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not load your submissions.")
		return
	}

	var assignments []models.Assignment
	h.db.Select("name", "course").Find(&assignments)
	courseOf := make(map[string]string, len(assignments))
	for _, a := range assignments {
		courseOf[a.Name] = a.Course
	}

	result := make([]mySubmission, 0, len(submissions))
	for _, s := range submissions {
		course := courseOf[s.Assignment]
		if course == "" {
			course = courseOf[s.AssignmentName]
		}
		result = append(result, mySubmission{AssignmentSubmission: s, Course: course})
	}
	httpx.WriteData(w, http.StatusOK, result)
}

// GET /portal/my-ca — continuous assessment per enrolled course.
func (h *LearnerHandler) MyCA(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}
	httpx.WriteData(w, http.StatusOK, h.ca.ForLearner(email))
}

// GET /portal/my-enrollments — enrollments with their review status.
func (h *LearnerHandler) MyEnrollments(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}
	var rows []models.StudentEnrollment
	h.db.Where("LOWER(email) = ?", email).Order("created_at DESC").Find(&rows)
	httpx.WriteData(w, http.StatusOK, rows)
}

// GET /portal/me
func (h *LearnerHandler) Me(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}
	var me models.Learner
	if h.db.Where("LOWER(email) = ?", email).First(&me).Error != nil {
		httpx.WriteError(w, http.StatusNotFound, "Account not found.")
		return
	}
	httpx.WriteData(w, http.StatusOK, me)
}

// PATCH /portal/me — the learner updates their own profile.
func (h *LearnerHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}
	var req map[string]any
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	allowed := map[string]bool{
		"first_name": true, "last_name": true, "contact_no": true,
		"gender": true, "district": true, "cooperative": true, "sms_opt_in": true,
	}
	updates := map[string]any{}
	for key, value := range req {
		if allowed[key] {
			if s, isString := value.(string); isString {
				value = strings.TrimSpace(s)
			}
			updates[key] = value
		}
	}
	if name, isString := updates["first_name"].(string); isString && name == "" {
		httpx.WriteError(w, http.StatusBadRequest, "First name can't be empty.")
		return
	}
	if len(updates) > 0 {
		if err := h.db.Model(&models.Learner{}).Where("LOWER(email) = ?", email).Updates(updates).Error; err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not update your profile.")
			return
		}
	}
	h.Me(w, r)
}

// GET /portal/notifications — latest notifications for the signed-in account.
func (h *LearnerHandler) Notifications(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}
	var rows []models.Notification
	h.db.Where("recipient_email = ?", email).Order("created_at DESC").Limit(50).Find(&rows)
	var unread int64
	h.db.Model(&models.Notification{}).Where("recipient_email = ? AND read_at IS NULL", email).Count(&unread)
	httpx.WriteData(w, http.StatusOK, map[string]any{"items": rows, "unread": unread})
}

// POST /portal/notifications/read — body {"ids": [..]} or {} for all.
func (h *LearnerHandler) MarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}
	var req struct {
		IDs []uint `json:"ids"`
	}
	_ = httpx.DecodeJSON(r, &req)
	query := h.db.Model(&models.Notification{}).Where("recipient_email = ? AND read_at IS NULL", email)
	if len(req.IDs) > 0 {
		query = query.Where("id IN ?", req.IDs)
	}
	query.Update("read_at", time.Now())
	httpx.WriteData(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /portal/announcements — for the learner's courses plus site-wide ones.
func (h *LearnerHandler) Announcements(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}
	var courses []string
	h.db.Model(&models.StudentEnrollment{}).
		Where("LOWER(email) = ? AND (status IS NULL OR status <> 'rejected')", email).
		Distinct("course").Pluck("course", &courses)

	var rows []models.Announcement
	query := h.db.Where("course = '' OR course IS NULL")
	if len(courses) > 0 {
		query = h.db.Where("course = '' OR course IS NULL OR course IN ?", courses)
	}
	query.Order("created_at DESC").Limit(20).Find(&rows)
	httpx.WriteData(w, http.StatusOK, rows)
}

// GET /portal/verify-certificate/{code} — public certificate check (no login).
func (h *LearnerHandler) VerifyCertificate(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "code")))
	var cert models.Certificate
	if code == "" || h.db.Where("UPPER(code) = ? AND status = 'earned'", code).First(&cert).Error != nil {
		httpx.WriteData(w, http.StatusOK, map[string]any{"valid": false, "code": code})
		return
	}
	var learner models.Learner
	name := cert.LearnerEmail
	if h.db.Where("LOWER(email) = LOWER(?)", cert.LearnerEmail).First(&learner).Error == nil {
		name = learner.FullName()
	}
	var course models.Course
	lecturerName := ""
	if h.db.Where("name = ?", cert.Course).First(&course).Error == nil && course.Lecturer != "" {
		var lecturer models.Learner
		if h.db.Where("LOWER(email) = LOWER(?)", course.Lecturer).First(&lecturer).Error == nil {
			lecturerName = lecturer.FullName()
		}
	}
	httpx.WriteData(w, http.StatusOK, map[string]any{
		"valid":       true,
		"code":        cert.Code,
		"learner":     name,
		"course":      cert.Course,
		"duration":    course.Duration,
		"issued_date": cert.IssuedDate,
		"ca_percent":  cert.CAPercent,
		"lecturer":    lecturerName,
		"detail":      cert.Detail,
	})
}
