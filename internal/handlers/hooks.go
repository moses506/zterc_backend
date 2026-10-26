package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"gorm.io/gorm"

	"zetrc-backend/internal/models"
	"zetrc-backend/internal/notify"
)

// snapshot turns a record into a plain map (JSON field names) for diffs and audit.
func snapshot(instance any) map[string]any {
	raw, _ := json.Marshal(instance)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// changedFields returns {field: {"from": x, "to": y}} for fields that changed.
func changedFields(before, after map[string]any) map[string]any {
	changes := map[string]any{}
	for key, next := range after {
		if key == "modified" || key == "creation" {
			continue
		}
		if prev, ok := before[key]; !ok || !reflect.DeepEqual(prev, next) {
			changes[key] = map[string]any{"from": before[key], "to": next}
		}
	}
	return changes
}

// recordAudit writes an audit entry for staff actions. Learner and anonymous
// requests are not audited here (their own records carry who/when).
func recordAudit(db *gorm.DB, r *http.Request, action, model, record string, details any) {
	claims := ClaimsFrom(r)
	if claims == nil || (claims.Role != models.RoleAdmin && claims.Role != models.RoleLecturer) {
		return
	}
	payload, _ := json.Marshal(scrubSecrets(details))
	changes := string(payload)
	if len(changes) > 8000 {
		changes = changes[:8000] + "…"
	}
	db.Create(&models.AuditLog{
		ActorEmail: strings.ToLower(claims.Email),
		ActorRole:  claims.Role,
		Action:     action,
		Model:      model,
		Record:     record,
		Summary:    auditSummary(action, model, record, details),
		Changes:    changes,
	})
}

func scrubSecrets(details any) any {
	if m, ok := details.(map[string]any); ok {
		clean := make(map[string]any, len(m))
		for k, v := range m {
			switch strings.ToLower(k) {
			case "password", "password_hash", "token":
				clean[k] = "••••"
			case "attachment", "documents":
				if s, ok := v.(string); ok && strings.HasPrefix(s, "data:") {
					clean[k] = "(file)"
					continue
				}
				clean[k] = v
			default:
				clean[k] = v
			}
		}
		return clean
	}
	return details
}

func auditSummary(action, model, record string, details any) string {
	label := strings.ReplaceAll(model, "_", " ")
	switch action {
	case "update":
		if m, ok := details.(map[string]any); ok && len(m) > 0 {
			fields := make([]string, 0, len(m))
			for k := range m {
				fields = append(fields, k)
			}
			return fmt.Sprintf("Updated %s %s (%s)", label, record, strings.Join(fields, ", "))
		}
		return fmt.Sprintf("Updated %s %s", label, record)
	case "create":
		return fmt.Sprintf("Created %s %s", label, record)
	case "delete":
		return fmt.Sprintf("Deleted %s %s", label, record)
	}
	return fmt.Sprintf("%s %s %s", strings.Title(action), label, record) //nolint:staticcheck
}

func (h *CRUDHandler) audit(r *http.Request, action, model, record string, details any) {
	recordAudit(h.db, r, action, model, record, details)
}

// courseLearners lists emails of learners enrolled in a course (not rejected).
func courseLearners(db *gorm.DB, course string) []string {
	var emails []string
	db.Model(&models.StudentEnrollment{}).
		Where("course = ? AND (status IS NULL OR status <> 'rejected')", course).
		Distinct("email").Pluck("email", &emails)
	return emails
}

// allLearners lists every active learner account.
func allLearners(db *gorm.DB) []string {
	var emails []string
	db.Model(&models.Learner{}).Where("role = ? AND active = ?", models.RoleLearner, true).Pluck("email", &emails)
	return emails
}

func courseOfModule(db *gorm.DB, module string) string {
	var cm models.CourseModule
	if db.Where("name = ? OR LOWER(title) = LOWER(?)", module, module).First(&cm).Error == nil {
		return cm.Course
	}
	return ""
}

func courseOfAssignment(db *gorm.DB, assignment, assignmentName string) string {
	var a models.Assignment
	if db.Where("name = ? OR name = ?", assignment, assignmentName).First(&a).Error == nil {
		return a.Course
	}
	return ""
}

// beforeCreate fills server-controlled fields.
func (h *CRUDHandler) beforeCreate(r *http.Request, model string, instance any) {
	switch record := instance.(type) {
	case *models.AssignmentSubmission:
		if strings.TrimSpace(record.Assignment) == "" {
			record.Assignment = strings.TrimSpace(record.AssignmentName)
		}
		if record.Status == "" {
			record.Status = "submitted"
		}
	case *models.Announcement:
		if claims := ClaimsFrom(r); claims != nil {
			record.AuthorEmail = strings.ToLower(claims.Email)
			var author models.Learner
			if h.db.Where("LOWER(email) = ?", record.AuthorEmail).First(&author).Error == nil {
				record.AuthorName = author.FullName()
			}
		}
		if record.Priority == "" {
			record.Priority = "info"
		}
	case *models.Quiz:
		if record.Status == "" {
			record.Status = "draft"
		}
	}
}

// afterCreate sends notifications for new content.
func (h *CRUDHandler) afterCreate(model string, instance any) {
	switch record := instance.(type) {
	case *models.LessonContent:
		course := courseOfModule(h.db, record.Module)
		if course == "" {
			return
		}
		h.notifier.Notify(courseLearners(h.db, course), notify.Message{
			Kind:  "lesson",
			Title: "New lesson available",
			Body:  fmt.Sprintf("A new lesson was added to %s.", course),
			Link:  "training",
			SMS:   true,
		})
	case *models.Announcement:
		recipients := allLearners(h.db)
		if record.Course != "" {
			recipients = courseLearners(h.db, record.Course)
		}
		h.notifier.Notify(recipients, notify.Message{
			Kind:  "announcement",
			Title: record.Title,
			Body:  truncate(record.Body, 160),
			Link:  "dashboard",
			SMS:   true,
		})
	case *models.Quiz:
		if record.Status == "published" {
			h.notifyQuizPublished(record)
		}
	}
}

// afterUpdate reacts to marking, publishing and similar changes.
func (h *CRUDHandler) afterUpdate(model string, before map[string]any, instance any) {
	switch record := instance.(type) {
	case *models.AssignmentSubmission:
		prevStatus, _ := before["status"].(string)
		prevScore, _ := before["score"].(float64)
		marked := record.Status == "marked" || record.Status == "returned"
		if !marked || (prevStatus == record.Status && prevScore == record.Score) {
			return
		}
		title := "Assignment marked"
		if record.Status == "returned" {
			title = "Assignment returned for changes"
		}
		body := record.AssignmentName
		if record.MaxScore > 0 {
			body = fmt.Sprintf("%s: %g/%g", record.AssignmentName, record.Score, record.MaxScore)
		}
		h.notifier.Notify([]string{record.Student}, notify.Message{
			Kind: "marked", Title: title, Body: body, Link: "assignments", SMS: true,
		})
		if course := courseOfAssignment(h.db, record.Assignment, record.AssignmentName); course != "" {
			h.ca.EvaluateCertificate(record.Student, course)
		}
	case *models.Quiz:
		if prev, _ := before["status"].(string); prev != "published" && record.Status == "published" {
			h.notifyQuizPublished(record)
		}
	}
}

func (h *CRUDHandler) notifyQuizPublished(quiz *models.Quiz) {
	h.notifier.Notify(courseLearners(h.db, quiz.Course), notify.Message{
		Kind:  "quiz",
		Title: "New quiz: " + quiz.Title,
		Body:  "A quiz is ready in " + quiz.Course + ". It counts towards your CA.",
		Link:  "quizzes",
		SMS:   true,
	})
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
