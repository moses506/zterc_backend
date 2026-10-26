package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"zetrc-backend/internal/httpx"
	"zetrc-backend/internal/models"
	"zetrc-backend/internal/notify"
)

// CommunityHandler covers enrollment review, course discussions and the
// learners' field photo journal.
type CommunityHandler struct {
	db       *gorm.DB
	notifier *notify.Service
}

func NewCommunityHandler(db *gorm.DB, notifier *notify.Service) *CommunityHandler {
	return &CommunityHandler{db: db, notifier: notifier}
}

func (h *CommunityHandler) accountName(email string) string {
	var account models.Learner
	if h.db.Where("LOWER(email) = LOWER(?)", email).First(&account).Error == nil {
		return account.FullName()
	}
	return email
}

// ---------- Enrollment review (admin) ----------

// POST /admin/api/enrollments/review — {"name": "...", "decision": "approved|rejected", "note": "..."}
func (h *CommunityHandler) ReviewEnrollment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if req.Decision != "approved" && req.Decision != "rejected" && req.Decision != "pending" {
		httpx.WriteError(w, http.StatusBadRequest, "Decision must be approved, rejected or pending.")
		return
	}
	if req.Decision == "rejected" && strings.TrimSpace(req.Note) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "Please give the applicant a reason for rejecting.")
		return
	}

	var enrollment models.StudentEnrollment
	if h.db.Where("name = ?", req.Name).First(&enrollment).Error != nil {
		httpx.WriteError(w, http.StatusNotFound, "Enrollment not found.")
		return
	}

	reviewer := ClaimsFrom(r).Email
	h.db.Model(&enrollment).Updates(map[string]any{
		"status": req.Decision, "review_note": strings.TrimSpace(req.Note), "reviewed_by": reviewer,
	})
	recordAudit(h.db, r, "review", "Student_Enrollment", enrollment.Name, map[string]any{
		"decision": req.Decision, "note": req.Note, "learner": enrollment.Email, "course": enrollment.Course,
	})

	switch req.Decision {
	case "approved":
		h.notifier.Notify([]string{enrollment.Email}, notify.Message{
			Kind: "enrollment", Title: "Enrollment approved",
			Body: fmt.Sprintf("Welcome to %s — you can start learning now.", enrollment.Course),
			Link: "mycourse", SMS: true,
		})
	case "rejected":
		h.notifier.Notify([]string{enrollment.Email}, notify.Message{
			Kind: "enrollment", Title: "Enrollment not approved",
			Body: fmt.Sprintf("%s: %s", enrollment.Course, strings.TrimSpace(req.Note)),
			Link: "mycourse", SMS: true,
		})
	}

	h.db.Where("name = ?", req.Name).First(&enrollment)
	httpx.WriteData(w, http.StatusOK, enrollment)
}

// ---------- Discussions (any signed-in account) ----------

// accessibleCourses: admins → nil (all), lecturers → own, learners → enrolled.
func (h *CommunityHandler) accessibleCourses(r *http.Request) (all bool, courses []string) {
	claims := ClaimsFrom(r)
	email := strings.ToLower(claims.Email)
	switch claims.Role {
	case models.RoleAdmin:
		return true, nil
	case models.RoleLecturer:
		h.db.Model(&models.Course{}).Where("LOWER(lecturer) = ?", email).Pluck("name", &courses)
	default:
		h.db.Model(&models.StudentEnrollment{}).
			Where("LOWER(email) = ? AND (status IS NULL OR status <> 'rejected')", email).
			Distinct("course").Pluck("course", &courses)
	}
	return false, courses
}

func (h *CommunityHandler) canUseCourse(r *http.Request, course string) bool {
	all, courses := h.accessibleCourses(r)
	if all {
		return true
	}
	for _, c := range courses {
		if c == course {
			return true
		}
	}
	return false
}

func isStaff(r *http.Request) bool {
	claims := ClaimsFrom(r)
	return claims != nil && (claims.Role == models.RoleAdmin || claims.Role == models.RoleLecturer)
}

// GET /portal/discussions?course=X
func (h *CommunityHandler) ListThreads(w http.ResponseWriter, r *http.Request) {
	course := strings.TrimSpace(r.URL.Query().Get("course"))
	all, courses := h.accessibleCourses(r)

	query := h.db.Order("last_activity DESC").Limit(200)
	switch {
	case course != "":
		if !h.canUseCourse(r, course) {
			httpx.WriteError(w, http.StatusForbidden, "You don't have access to this course.")
			return
		}
		query = query.Where("course = ?", course)
	case !all:
		if len(courses) == 0 {
			httpx.WriteData(w, http.StatusOK, map[string]any{"threads": []models.DiscussionThread{}, "courses": []string{}})
			return
		}
		query = query.Where("course IN ?", courses)
	}

	var threads []models.DiscussionThread
	query.Find(&threads)
	if all {
		h.db.Model(&models.Course{}).Order("name").Pluck("name", &courses)
	}
	httpx.WriteData(w, http.StatusOK, map[string]any{"threads": threads, "courses": courses})
}

// POST /portal/discussions — {"course", "title", "body"}
func (h *CommunityHandler) CreateThread(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Course string `json:"course"`
		Title  string `json:"title"`
		Body   string `json:"body"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	req.Title, req.Body = strings.TrimSpace(req.Title), strings.TrimSpace(req.Body)
	if req.Title == "" || req.Body == "" {
		httpx.WriteError(w, http.StatusBadRequest, "Please add a title and your question.")
		return
	}
	if !h.canUseCourse(r, req.Course) {
		httpx.WriteError(w, http.StatusForbidden, "You can only post in your own courses.")
		return
	}

	claims := ClaimsFrom(r)
	thread := models.DiscussionThread{
		Course: req.Course, Title: req.Title, Body: req.Body,
		AuthorEmail: strings.ToLower(claims.Email), AuthorName: h.accountName(claims.Email), AuthorRole: claims.Role,
		LastActivity: time.Now(),
	}
	if err := h.db.Create(&thread).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not post your question.")
		return
	}

	// Learner questions notify the course lecturer; staff posts notify learners.
	if claims.Role == models.RoleLearner {
		var course models.Course
		if h.db.Where("name = ?", req.Course).First(&course).Error == nil && course.Lecturer != "" {
			h.notifier.Notify([]string{course.Lecturer}, notify.Message{
				Kind: "discussion", Title: "New question in " + req.Course,
				Body: thread.AuthorName + ": " + truncate(thread.Title, 100), Link: "discussions",
			})
		}
	} else {
		h.notifier.Notify(courseLearners(h.db, req.Course), notify.Message{
			Kind: "discussion", Title: "New discussion in " + req.Course, Body: truncate(thread.Title, 100), Link: "discussions",
		})
	}
	httpx.WriteData(w, http.StatusCreated, thread)
}

func (h *CommunityHandler) loadThread(w http.ResponseWriter, r *http.Request) (*models.DiscussionThread, bool) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid discussion id.")
		return nil, false
	}
	var thread models.DiscussionThread
	if h.db.First(&thread, id).Error != nil {
		httpx.WriteError(w, http.StatusNotFound, "Discussion not found.")
		return nil, false
	}
	if !h.canUseCourse(r, thread.Course) {
		httpx.WriteError(w, http.StatusForbidden, "You don't have access to this discussion.")
		return nil, false
	}
	return &thread, true
}

// GET /portal/discussions/{id}
func (h *CommunityHandler) GetThread(w http.ResponseWriter, r *http.Request) {
	thread, ok := h.loadThread(w, r)
	if !ok {
		return
	}
	var replies []models.DiscussionReply
	h.db.Where("thread_id = ?", thread.ID).Order("created_at").Find(&replies)
	httpx.WriteData(w, http.StatusOK, map[string]any{"thread": thread, "replies": replies})
}

// POST /portal/discussions/{id}/replies — {"body"}
func (h *CommunityHandler) Reply(w http.ResponseWriter, r *http.Request) {
	thread, ok := h.loadThread(w, r)
	if !ok {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Body) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "Please write a reply.")
		return
	}
	claims := ClaimsFrom(r)
	reply := models.DiscussionReply{
		ThreadID: thread.ID, Body: strings.TrimSpace(req.Body),
		AuthorEmail: strings.ToLower(claims.Email), AuthorName: h.accountName(claims.Email), AuthorRole: claims.Role,
	}
	if err := h.db.Create(&reply).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not post your reply.")
		return
	}
	h.db.Model(thread).Updates(map[string]any{"reply_count": gorm.Expr("reply_count + 1"), "last_activity": time.Now()})

	if thread.AuthorEmail != reply.AuthorEmail {
		title := reply.AuthorName + " replied to your question"
		if reply.AuthorRole == models.RoleLecturer {
			title = "Your lecturer replied to your question"
		}
		h.notifier.Notify([]string{thread.AuthorEmail}, notify.Message{
			Kind: "reply", Title: title, Body: truncate(thread.Title, 100), Link: "discussions",
			SMS: reply.AuthorRole != models.RoleLearner,
		})
	}
	httpx.WriteData(w, http.StatusCreated, reply)
}

// PATCH /portal/discussions/{id} — {"resolved": true}; author or staff.
func (h *CommunityHandler) UpdateThread(w http.ResponseWriter, r *http.Request) {
	thread, ok := h.loadThread(w, r)
	if !ok {
		return
	}
	if !isStaff(r) && !strings.EqualFold(ClaimsFrom(r).Email, thread.AuthorEmail) {
		httpx.WriteError(w, http.StatusForbidden, "Only the author or staff can change this.")
		return
	}
	var req struct {
		Resolved *bool `json:"resolved"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Resolved == nil {
		httpx.WriteError(w, http.StatusBadRequest, "Nothing to update.")
		return
	}
	h.db.Model(thread).Update("resolved", *req.Resolved)
	httpx.WriteData(w, http.StatusOK, thread)
}

// DELETE /portal/discussions/{id} — staff moderation or the author.
func (h *CommunityHandler) DeleteThread(w http.ResponseWriter, r *http.Request) {
	thread, ok := h.loadThread(w, r)
	if !ok {
		return
	}
	if !isStaff(r) && !strings.EqualFold(ClaimsFrom(r).Email, thread.AuthorEmail) {
		httpx.WriteError(w, http.StatusForbidden, "Only the author or staff can delete this.")
		return
	}
	h.db.Where("thread_id = ?", thread.ID).Delete(&models.DiscussionReply{})
	h.db.Delete(thread)
	recordAudit(h.db, r, "delete", "Discussion", thread.Title, map[string]any{"course": thread.Course})
	httpx.WriteData(w, http.StatusOK, map[string]any{"deleted": true})
}

// ---------- Field photo journal ----------

// GET /portal/field-photos — the learner's own photos.
func (h *CommunityHandler) MyPhotos(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}
	var photos []models.FieldPhoto
	h.db.Where("learner_email = ?", email).Order("created_at DESC").Find(&photos)
	httpx.WriteData(w, http.StatusOK, photos)
}

// POST /portal/field-photos — {"course","caption","image_url","latitude","longitude","taken_at"}
func (h *CommunityHandler) AddPhoto(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}
	var photo models.FieldPhoto
	if err := httpx.DecodeJSON(r, &photo); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if !strings.HasPrefix(photo.ImageURL, "/uploads/") {
		httpx.WriteError(w, http.StatusBadRequest, "Upload a photo first.")
		return
	}
	photo.ID = 0
	photo.LearnerEmail = email
	photo.LearnerName = h.accountName(email)
	photo.Caption = strings.TrimSpace(photo.Caption)
	if photo.Course != "" && !h.canUseCourse(r, photo.Course) {
		photo.Course = ""
	}
	if err := h.db.Create(&photo).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not save your photo.")
		return
	}
	httpx.WriteData(w, http.StatusCreated, photo)
}

// DELETE /portal/field-photos/{id} — own photos only.
func (h *CommunityHandler) DeletePhoto(w http.ResponseWriter, r *http.Request) {
	email, ok := signedInEmail(w, r)
	if !ok {
		return
	}
	result := h.db.Where("id = ? AND learner_email = ?", chi.URLParam(r, "id"), email).Delete(&models.FieldPhoto{})
	if result.RowsAffected == 0 {
		httpx.WriteError(w, http.StatusNotFound, "Photo not found.")
		return
	}
	httpx.WriteData(w, http.StatusOK, map[string]any{"deleted": true})
}

// GET /admin/api/field-photos and /lecturer/api/field-photos (lecturer: own courses).
func (h *CommunityHandler) StaffPhotos(w http.ResponseWriter, r *http.Request) {
	all, courses := h.accessibleCourses(r)
	query := h.db.Order("created_at DESC").Limit(500)
	if course := r.URL.Query().Get("course"); course != "" {
		query = query.Where("course = ?", course)
	}
	if !all {
		if len(courses) == 0 {
			httpx.WriteData(w, http.StatusOK, []models.FieldPhoto{})
			return
		}
		// Photos tagged with one of the lecturer's courses, or by their enrolled learners.
		var learners []string
		h.db.Model(&models.StudentEnrollment{}).Where("course IN ?", courses).Pluck("LOWER(email)", &learners)
		query = query.Where("course IN ? OR (course = '' AND learner_email IN ?)", courses, learners)
	}
	var photos []models.FieldPhoto
	query.Find(&photos)
	httpx.WriteData(w, http.StatusOK, photos)
}
