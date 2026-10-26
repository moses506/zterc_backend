package handlers

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"zetrc-backend/internal/httpx"
	"zetrc-backend/internal/models"
)

// ImpactHandler builds the funder-facing impact report (admin only).
type ImpactHandler struct {
	db *gorm.DB
	ca *CAService
}

func NewImpactHandler(db *gorm.DB, ca *CAService) *ImpactHandler {
	return &ImpactHandler{db: db, ca: ca}
}

type countRow struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type courseImpact struct {
	Course       string   `json:"course"`
	Enrollments  int      `json:"enrollments"`
	Certificates int      `json:"certificates"`
	Completion   float64  `json:"completion"`
	AverageCA    *float64 `json:"average_ca"`
}

func groupCounts(db *gorm.DB, column string, roleLearner bool) []countRow {
	var rows []countRow
	q := db.Model(&models.Learner{}).Select("COALESCE(NULLIF(TRIM(" + column + "), ''), 'Not stated') AS label, COUNT(*) AS count")
	if roleLearner {
		q = q.Where("role = ?", models.RoleLearner)
	}
	q.Group("label").Order("count DESC").Scan(&rows)
	return rows
}

// GET /admin/api/impact
func (h *ImpactHandler) Report(w http.ResponseWriter, r *http.Request) {
	var learners, active30, enrollments, approved, pending, certificates, photos, submissions, quizAttempts int64
	h.db.Model(&models.Learner{}).Where("role = ?", models.RoleLearner).Count(&learners)
	since := time.Now().AddDate(0, 0, -30)
	h.db.Model(&models.LessonProgress{}).Where("updated_at >= ?", since).Distinct("learner_email").Count(&active30)
	h.db.Model(&models.StudentEnrollment{}).Count(&enrollments)
	h.db.Model(&models.StudentEnrollment{}).Where("status IS NULL OR status = 'approved'").Count(&approved)
	h.db.Model(&models.StudentEnrollment{}).Where("status = 'pending'").Count(&pending)
	h.db.Model(&models.Certificate{}).Where("status = 'earned'").Count(&certificates)
	h.db.Model(&models.FieldPhoto{}).Count(&photos)
	h.db.Model(&models.AssignmentSubmission{}).Count(&submissions)
	h.db.Model(&models.QuizAttempt{}).Count(&quizAttempts)

	// Per course: enrollments, certificates, completion and average CA.
	var courses []models.Course
	h.db.Order("name").Find(&courses)
	byCourse := []courseImpact{}
	var caSum float64
	var caCount int
	for _, c := range courses {
		var enrolled []models.StudentEnrollment
		h.db.Where("course = ? AND (status IS NULL OR status <> 'rejected')", c.Name).Find(&enrolled)
		var certs int64
		h.db.Model(&models.Certificate{}).Where("course = ? AND status = 'earned'", c.Name).Count(&certs)

		row := courseImpact{Course: c.Name, Enrollments: len(enrolled), Certificates: int(certs)}
		if len(enrolled) > 0 {
			row.Completion = float64(certs) / float64(len(enrolled)) * 100
		}
		var sum float64
		var n int
		seen := map[string]bool{}
		for _, e := range enrolled {
			email := strings.ToLower(e.Email)
			if seen[email] {
				continue
			}
			seen[email] = true
			if ca := h.ca.ForCourse(email, c.Name); ca.Percent != nil {
				sum += *ca.Percent
				n++
			}
		}
		if n > 0 {
			avg := sum / float64(n)
			row.AverageCA = &avg
			caSum += sum
			caCount += n
		}
		byCourse = append(byCourse, row)
	}

	// Enrollments per month for the last 12 months.
	type monthRow struct {
		Month string `json:"month"`
		Count int    `json:"count"`
	}
	var monthly []monthRow
	h.db.Model(&models.StudentEnrollment{}).
		Select("TO_CHAR(DATE_TRUNC('month', created_at), 'YYYY-MM') AS month, COUNT(*) AS count").
		Where("created_at >= ?", time.Now().AddDate(-1, 0, 0)).
		Group("month").Order("month").Scan(&monthly)

	var averageCA *float64
	if caCount > 0 {
		v := caSum / float64(caCount)
		averageCA = &v
	}
	completion := 0.0
	if approved > 0 {
		completion = float64(certificates) / float64(approved) * 100
	}

	sort.SliceStable(byCourse, func(i, j int) bool { return byCourse[i].Enrollments > byCourse[j].Enrollments })

	httpx.WriteData(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().Format(time.RFC3339),
		"totals": map[string]any{
			"learners": learners, "active_30d": active30, "enrollments": enrollments,
			"approved": approved, "pending": pending, "certificates": certificates,
			"completion_rate": completion, "average_ca": averageCA,
			"field_photos": photos, "submissions": submissions, "quiz_attempts": quizAttempts,
		},
		"by_gender":   groupCounts(h.db, "gender", true),
		"by_district": groupCounts(h.db, "district", true),
		"by_course":   byCourse,
		"by_month":    monthly,
	})
}

// GET /admin/api/audit-log?model=&actor=&limit=
func (h *ImpactHandler) AuditLog(w http.ResponseWriter, r *http.Request) {
	query := h.db.Order("created_at DESC").Limit(300)
	if model := r.URL.Query().Get("model"); model != "" {
		query = query.Where("model = ?", model)
	}
	if actor := r.URL.Query().Get("actor"); actor != "" {
		query = query.Where("actor_email = ?", strings.ToLower(actor))
	}
	var rows []models.AuditLog
	query.Find(&rows)
	httpx.WriteData(w, http.StatusOK, rows)
}
