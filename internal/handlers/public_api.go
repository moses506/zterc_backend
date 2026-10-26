package handlers

import (
	"net/http"
	"strings"

	"gorm.io/gorm"

	"zetrc-backend/internal/httpx"
)

// PublicAPIHandler guards the /api/* endpoints used by the public website and
// the learner dashboard:
//
//   - course content (courses, modules, lessons, assignments) is readable by anyone;
//   - a learner's own enrollments, certificates and submissions need their login
//     and are always filtered to that learner;
//   - contact forms (leads, inquiries, proposal requests) can be posted anonymously;
//   - enrollments and submissions can only be created by the signed-in learner;
//   - nothing can be edited or deleted here — staff use /admin/api and /lecturer/api.
type PublicAPIHandler struct {
	db   *gorm.DB
	crud *CRUDHandler
}

func NewPublicAPIHandler(db *gorm.DB, crud *CRUDHandler) *PublicAPIHandler {
	return &PublicAPIHandler{db: db, crud: crud}
}

var publicReadable = map[string]bool{
	"Course": true, "Course_Module": true, "Lesson_Content": true, "Assignment": true,
}

// ownerColumn is the column that ties a learner-owned record to its learner.
var ownerColumn = map[string]string{
	"Student_Enrollment":    "email",
	"Certificate":           "learner_email",
	"Assignment_Submission": "student",
}

var publicWritable = map[string]bool{"Lead": true, "Inquiry": true, "Proposal_Request": true}

// GET /api/get-data/
func (h *PublicAPIHandler) Get(w http.ResponseWriter, r *http.Request) {
	model := modelFromRequest(r)
	if publicReadable[model] {
		h.crud.getRecords(w, r, nil)
		return
	}
	column, owned := ownerColumn[model]
	if !owned {
		httpx.WriteError(w, http.StatusForbidden, "This data is only available to staff.")
		return
	}
	claims := ClaimsFrom(r)
	if claims == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "Please sign in to view this.")
		return
	}
	email := strings.ToLower(claims.Email)
	h.crud.getRecords(w, r, func(q *gorm.DB) *gorm.DB {
		return q.Where("LOWER("+column+") = ?", email)
	})
}

// POST /api/post-data/
func (h *PublicAPIHandler) Create(w http.ResponseWriter, r *http.Request) {
	req, err := h.crud.readCRUDRequest(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	if publicWritable[req.Model] {
		h.crud.createRecord(w, r, req)
		return
	}

	column, owned := ownerColumn[req.Model]
	if !owned || req.Model == "Certificate" {
		httpx.WriteError(w, http.StatusForbidden, "You can't create this type of record here.")
		return
	}
	claims := ClaimsFrom(r)
	if claims == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "Please sign in again to submit this.")
		return
	}

	// The record always belongs to the signed-in learner.
	req.Body[column] = strings.ToLower(claims.Email)
	delete(req.Body, "name_override")
	switch req.Model {
	case "Student_Enrollment":
		req.Body["status"] = "pending"
		delete(req.Body, "review_note")
		delete(req.Body, "reviewed_by")
	case "Assignment_Submission":
		req.Body["status"] = "submitted"
		for _, field := range []string{"score", "max_score", "grade", "feedback"} {
			delete(req.Body, field)
		}
	}
	h.crud.createRecord(w, r, req)
}

// Blocked is used for edit/delete routes that no longer exist on /api.
func (h *PublicAPIHandler) Blocked(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, http.StatusForbidden, "Editing is only available in the admin console or lecturer portal.")
}
