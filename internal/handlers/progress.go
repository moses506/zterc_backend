package handlers

import (
	"errors"
	"math"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"zetrc-backend/internal/httpx"
	"zetrc-backend/internal/models"
)

// ProgressHandler records learner lesson progress and reports it to staff.
type ProgressHandler struct {
	db *gorm.DB
}

func NewProgressHandler(db *gorm.DB) *ProgressHandler {
	return &ProgressHandler{db: db}
}

type progressUpdate struct {
	Key       string  `json:"key"`
	Module    string  `json:"module"`
	Percent   float64 `json:"percent"`
	Completed bool    `json:"completed"`
}

// A new history row is written when progress grows by at least this much.
const progressLogStep = 5.0

// courseForModule maps a lesson's module (record name or title, any case)
// or a course name to a course. Returns "" when nothing matches.
func (h *ProgressHandler) courseForModule(module string) string {
	module = strings.TrimSpace(module)
	if module == "" {
		return ""
	}
	var cm models.CourseModule
	if h.db.Where("LOWER(name) = LOWER(?) OR LOWER(TRIM(title)) = LOWER(?)", module, module).
		First(&cm).Error == nil && cm.Course != "" {
		return cm.Course
	}
	var course models.Course
	if h.db.Where("LOWER(name) = LOWER(?)", module).First(&course).Error == nil {
		return course.Name
	}
	return ""
}

// resolveCourse prefers the lesson's module and falls back to the learner's
// most recent enrollment when the module is not linked to a course.
func (h *ProgressHandler) resolveCourse(email, module string) string {
	if course := h.courseForModule(module); course != "" {
		return course
	}
	var enrollment models.StudentEnrollment
	if h.db.Where("LOWER(email) = LOWER(?)", email).Order("created_at DESC").First(&enrollment).Error == nil {
		return enrollment.Course
	}
	return ""
}

// backfillCourses re-assigns progress to the right course. Once a module is
// linked to a course, rows that were guessed from an enrollment move over;
// rows with no course at all get the enrollment fallback.
func (h *ProgressHandler) backfillCourses() {
	var modules []string
	h.db.Model(&models.LessonProgress{}).Distinct("module").Pluck("module", &modules)

	for _, module := range modules {
		course := h.courseForModule(module)
		if course == "" {
			continue
		}
		keys := h.db.Model(&models.LessonProgress{}).Select("key").Where("module = ?", module)
		h.db.Model(&models.ProgressLog{}).
			Where("key IN (?) AND course <> ?", keys, course).
			Update("course", course)
		h.db.Model(&models.LessonProgress{}).
			Where("module = ? AND course <> ?", module, course).
			Update("course", course)
	}

	var orphans []models.LessonProgress
	if h.db.Where("course = '' OR course IS NULL").Find(&orphans).Error != nil {
		return
	}
	for _, row := range orphans {
		course := h.resolveCourse(row.LearnerEmail, row.Module)
		if course == "" {
			continue
		}
		h.db.Model(&models.LessonProgress{}).Where("id = ?", row.ID).Update("course", course)
		h.db.Model(&models.ProgressLog{}).
			Where("learner_email = ? AND key = ? AND (course = '' OR course IS NULL)", row.LearnerEmail, row.Key).
			Update("course", course)
	}
}

// POST /portal/lesson-progress (learner token)
func (h *ProgressHandler) Record(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFrom(r)
	if claims == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "Please sign in again.")
		return
	}

	var req progressUpdate
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	if req.Key == "" {
		httpx.WriteError(w, http.StatusBadRequest, "A progress key is required.")
		return
	}
	req.Percent = math.Max(0, math.Min(100, req.Percent))
	if req.Completed {
		req.Percent = math.Max(req.Percent, 100)
	}

	email := strings.ToLower(claims.Email)
	course := h.resolveCourse(email, req.Module)

	err := h.db.Transaction(func(tx *gorm.DB) error {
		var row models.LessonProgress
		err := tx.Where("learner_email = ? AND key = ?", email, req.Key).First(&row).Error

		previous := 0.0
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			row = models.LessonProgress{
				LearnerEmail: email,
				Key:          req.Key,
				Module:       req.Module,
				Course:       course,
				Percent:      req.Percent,
				Completed:    req.Completed,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			previous = row.Percent
			if req.Percent <= row.Percent && (!req.Completed || row.Completed) {
				return nil // nothing new
			}
			updates := map[string]any{
				"percent":   math.Max(row.Percent, req.Percent),
				"completed": row.Completed || req.Completed,
			}
			if course != "" {
				updates["course"] = course
			}
			if err := tx.Model(&row).Updates(updates).Error; err != nil {
				return err
			}
		}

		if req.Percent-previous >= progressLogStep || (req.Completed && previous < 100) || previous == 0 {
			return tx.Create(&models.ProgressLog{
				LearnerEmail: email,
				Key:          req.Key,
				Course:       course,
				Percent:      req.Percent,
			}).Error
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not save progress.")
		return
	}

	httpx.WriteData(w, http.StatusOK, map[string]any{"saved": true})
}

type progressReport struct {
	Courses     []models.Course            `json:"courses"`
	Lessons     map[string]int             `json:"lessons_per_course"`
	Enrollments []models.StudentEnrollment `json:"enrollments"`
	Progress    []models.LessonProgress    `json:"progress"`
	Logs        []models.ProgressLog       `json:"logs"`
}

// GET /admin/api/progress and /lecturer/api/progress
func (h *ProgressHandler) Report(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFrom(r)
	if claims == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "Please sign in again.")
		return
	}

	h.backfillCourses()

	courseQuery := h.db.Order("name")
	if claims.Role == models.RoleLecturer {
		courseQuery = courseQuery.Where("lecturer = ?", strings.ToLower(claims.Email))
	}

	var report progressReport
	if err := courseQuery.Find(&report.Courses).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not load courses.")
		return
	}

	names := make([]string, 0, len(report.Courses))
	for _, course := range report.Courses {
		names = append(names, course.Name)
	}

	report.Lessons = map[string]int{}
	report.Enrollments = []models.StudentEnrollment{}
	report.Progress = []models.LessonProgress{}
	report.Logs = []models.ProgressLog{}

	if len(names) > 0 {
		type lessonCount struct {
			Course string
			Total  int
		}
		var counts []lessonCount
		h.db.Table("lesson_contents").
			Select("course_modules.course AS course, COUNT(*) AS total").
			Joins("JOIN course_modules ON course_modules.name = lesson_contents.module").
			Where("course_modules.course IN ?", names).
			Group("course_modules.course").
			Scan(&counts)
		for _, c := range counts {
			report.Lessons[c.Course] = c.Total
		}

		h.db.Where("course IN ?", names).Order("created_at").Find(&report.Enrollments)
		h.db.Where("course IN ?", names).Find(&report.Progress)
		h.db.Where("course IN ?", names).Order("created_at").Find(&report.Logs)
	}

	httpx.WriteData(w, http.StatusOK, report)
}
