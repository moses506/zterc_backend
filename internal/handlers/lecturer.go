package handlers

import (
	"net/http"
	"strings"

	"gorm.io/gorm"

	"zetrc-backend/internal/httpx"
	"zetrc-backend/internal/models"
)

// LecturerHandler exposes the generic CRUD endpoints under /lecturer/api,
// scoped to the courses assigned to the signed-in lecturer (Course.Lecturer).
type LecturerHandler struct {
	db   *gorm.DB
	crud *CRUDHandler
}

func NewLecturerHandler(db *gorm.DB, crud *CRUDHandler) *LecturerHandler {
	return &LecturerHandler{db: db, crud: crud}
}

// Fields a lecturer may change on records they can read but not fully edit.
var lecturerLimitedFields = map[string]map[string]bool{
	"Course": {"name": true, "description": true, "duration": true},
	"Assignment_Submission": {
		"name": true, "grade": true, "feedback": true, "status": true, "score": true, "max_score": true,
	},
}

// Models a lecturer may create and delete (within their own courses).
var lecturerOwnedModels = map[string]bool{
	"Course_Module":  true,
	"Lesson_Content": true,
	"Assignment":     true,
	"Certificate":    true,
	"Quiz":           true,
	"Announcement":   true,
}

func lecturerEmail(r *http.Request) string {
	if claims := ClaimsFrom(r); claims != nil {
		return strings.ToLower(claims.Email)
	}
	return ""
}

func (h *LecturerHandler) courseSubquery(email string) *gorm.DB {
	return h.db.Model(&models.Course{}).Select("name").Where("lecturer = ?", email)
}

func (h *LecturerHandler) moduleSubquery(email string) *gorm.DB {
	return h.db.Model(&models.CourseModule{}).Select("name").Where("course IN (?)", h.courseSubquery(email))
}

func (h *LecturerHandler) assignmentSubquery(email string) *gorm.DB {
	return h.db.Model(&models.Assignment{}).Select("name").Where("course IN (?)", h.courseSubquery(email))
}

// scopeFor returns the query filter limiting model to this lecturer's courses.
func (h *LecturerHandler) scopeFor(model, email string) (scopeFunc, bool) {
	switch model {
	case "Course":
		return func(q *gorm.DB) *gorm.DB { return q.Where("lecturer = ?", email) }, true
	case "Course_Module", "Assignment", "Certificate", "Student_Enrollment", "Quiz", "Announcement":
		return func(q *gorm.DB) *gorm.DB { return q.Where("course IN (?)", h.courseSubquery(email)) }, true
	case "Lesson_Content":
		return func(q *gorm.DB) *gorm.DB { return q.Where("module IN (?)", h.moduleSubquery(email)) }, true
	case "Assignment_Submission":
		// Older submissions were saved without the assignment link, so match on
		// the assignment name as well.
		return func(q *gorm.DB) *gorm.DB {
			return q.Where("assignment IN (?) OR assignment_name IN (?)", h.assignmentSubquery(email), h.assignmentSubquery(email))
		}, true
	}
	return nil, false
}

func (h *LecturerHandler) exists(query *gorm.DB) bool {
	var count int64
	query.Count(&count)
	return count > 0
}

func (h *LecturerHandler) ownsCourse(email, course string) bool {
	return course != "" && h.exists(h.db.Model(&models.Course{}).Where("name = ? AND lecturer = ?", course, email))
}

func (h *LecturerHandler) ownsModule(email, module string) bool {
	return module != "" && h.exists(h.db.Model(&models.CourseModule{}).
		Where("name = ? AND course IN (?)", module, h.courseSubquery(email)))
}

// parentAllowed checks the course/module a record is being attached to.
func (h *LecturerHandler) parentAllowed(model, email string, body map[string]any) (bool, string) {
	switch model {
	case "Lesson_Content":
		if module, ok := body["module"].(string); ok || body["module"] != nil {
			if !h.ownsModule(email, module) {
				return false, "You can only add lessons to modules in your own courses."
			}
		}
	case "Course_Module", "Assignment", "Certificate", "Quiz", "Announcement":
		if course, ok := body["course"].(string); ok || body["course"] != nil {
			if !h.ownsCourse(email, course) {
				return false, "You can only use courses assigned to you."
			}
		}
	}
	return true, ""
}

// ownsRecord reports whether the named record falls inside the lecturer's scope.
func (h *LecturerHandler) ownsRecord(model, email, name string) bool {
	scope, ok := h.scopeFor(model, email)
	if !ok {
		return false
	}
	instance, _ := models.NewInstance(model)
	return h.exists(scope(h.db.Model(instance)).Where("name = ?", name))
}

// GET /lecturer/api/get-data/?model=X
func (h *LecturerHandler) Get(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scopeFor(modelFromRequest(r), lecturerEmail(r))
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "Lecturers cannot view this data.")
		return
	}
	h.crud.getRecords(w, r, scope)
}

// POST /lecturer/api/post-data/
func (h *LecturerHandler) Create(w http.ResponseWriter, r *http.Request) {
	req, err := h.crud.readCRUDRequest(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	email := lecturerEmail(r)

	if !lecturerOwnedModels[req.Model] {
		httpx.WriteError(w, http.StatusForbidden, "Lecturers cannot create this type of record.")
		return
	}

	// A course/module is mandatory on create so the record stays in scope.
	key := "course"
	if req.Model == "Lesson_Content" {
		key = "module"
	}
	if value, _ := req.Body[key].(string); strings.TrimSpace(value) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "Please choose a "+key+".")
		return
	}
	if ok, msg := h.parentAllowed(req.Model, email, req.Body); !ok {
		httpx.WriteError(w, http.StatusForbidden, msg)
		return
	}

	if req.Model == "Assignment" {
		req.Body["owner"] = email
		if name, _ := req.Body["lecturer_name"].(string); strings.TrimSpace(name) == "" {
			var me models.Learner
			if h.db.Where("email = ?", email).First(&me).Error == nil {
				req.Body["lecturer_name"] = me.FullName()
			}
		}
	}

	h.crud.createRecord(w, r, req)
}

// PATCH /lecturer/api/patch-data/
func (h *LecturerHandler) Update(w http.ResponseWriter, r *http.Request) {
	req, err := h.crud.readCRUDRequest(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	email := lecturerEmail(r)

	allowedFields, limited := lecturerLimitedFields[req.Model]
	if !limited && !lecturerOwnedModels[req.Model] {
		httpx.WriteError(w, http.StatusForbidden, "Lecturers cannot edit this type of record.")
		return
	}

	name, _ := req.Body["name"].(string)
	if name == "" || !h.ownsRecord(req.Model, email, name) {
		httpx.WriteError(w, http.StatusNotFound, "Record not found.")
		return
	}

	if limited {
		for key := range req.Body {
			if !allowedFields[key] {
				delete(req.Body, key)
			}
		}
	} else if ok, msg := h.parentAllowed(req.Model, email, req.Body); !ok {
		httpx.WriteError(w, http.StatusForbidden, msg)
		return
	}

	h.crud.updateRecord(w, r, req)
}

// DELETE /lecturer/api/delete-doc/
func (h *LecturerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	req, err := h.crud.readCRUDRequest(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	if !lecturerOwnedModels[req.Model] {
		httpx.WriteError(w, http.StatusForbidden, "Lecturers cannot delete this type of record.")
		return
	}

	name, _ := req.Body["name"].(string)
	if name == "" || !h.ownsRecord(req.Model, lecturerEmail(r), name) {
		httpx.WriteError(w, http.StatusNotFound, "Record not found.")
		return
	}

	h.crud.deleteRecord(w, r, req)
}
