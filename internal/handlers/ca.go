package handlers

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"zetrc-backend/internal/models"
	"zetrc-backend/internal/notify"
)

// CAService computes continuous assessment and awards certificates.
//
// CA for a learner in a course = marks awarded ÷ marks available, across
// scored assignment submissions and the best attempt at each quiz.
type CAService struct {
	db       *gorm.DB
	notifier *notify.Service
	PassMark float64
}

func NewCAService(db *gorm.DB, notifier *notify.Service) *CAService {
	pass := 50.0
	if v, err := strconv.ParseFloat(os.Getenv("CA_PASS_MARK"), 64); err == nil && v > 0 && v <= 100 {
		pass = v
	}
	return &CAService{db: db, notifier: notifier, PassMark: pass}
}

// CAItem is one scored piece of work.
type CAItem struct {
	Key      string  `json:"key"`
	Title    string  `json:"title"`
	Kind     string  `json:"kind"` // assignment | quiz
	Score    float64 `json:"score"`
	MaxScore float64 `json:"max_score"`
	Status   string  `json:"status"`
}

// CourseCA is a learner's CA in one course.
type CourseCA struct {
	Course   string   `json:"course"`
	Percent  *float64 `json:"percent"`
	Score    float64  `json:"score"`
	MaxScore float64  `json:"max_score"`
	Assessed int      `json:"assessed"`
	PassMark float64  `json:"pass_mark"`
	Items    []CAItem `json:"items"`
}

// items gathers scored work for one learner in one course.
func (s *CAService) items(email, course string) []CAItem {
	email = strings.ToLower(email)
	var assignments []models.Assignment
	s.db.Where("course = ?", course).Find(&assignments)
	names := make([]string, 0, len(assignments))
	for _, a := range assignments {
		names = append(names, a.Name)
	}

	var items []CAItem
	if len(names) > 0 {
		var subs []models.AssignmentSubmission
		s.db.Where("LOWER(student) = ? AND (assignment IN ? OR assignment_name IN ?)", email, names, names).
			Order("updated_at DESC").Find(&subs)
		seen := map[string]bool{}
		for _, sub := range subs {
			key := sub.Assignment
			if key == "" {
				key = sub.AssignmentName
			}
			if seen[key] {
				continue // latest submission per assignment
			}
			seen[key] = true
			items = append(items, CAItem{
				Key: key, Title: sub.AssignmentName, Kind: "assignment",
				Score: sub.Score, MaxScore: sub.MaxScore, Status: sub.Status,
			})
		}
	}

	var attempts []models.QuizAttempt
	s.db.Where("LOWER(learner_email) = ? AND course = ?", email, course).Find(&attempts)
	best := map[string]models.QuizAttempt{}
	for _, a := range attempts {
		if cur, ok := best[a.Quiz]; !ok || a.Score > cur.Score {
			best[a.Quiz] = a
		}
	}
	for _, a := range best {
		items = append(items, CAItem{
			Key: a.Quiz, Title: a.QuizTitle, Kind: "quiz", Score: a.Score, MaxScore: a.MaxScore, Status: "marked",
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Title < items[j].Title })
	return items
}

// ForCourse returns the learner's CA in a course.
func (s *CAService) ForCourse(email, course string) CourseCA {
	items := s.items(email, course)
	result := CourseCA{Course: course, PassMark: s.PassMark, Items: items}
	for _, item := range items {
		if (item.Status == "marked" || item.Status == "returned") && item.MaxScore > 0 {
			result.Score += item.Score
			result.MaxScore += item.MaxScore
			result.Assessed++
		}
	}
	if result.MaxScore > 0 {
		pct := result.Score / result.MaxScore * 100
		result.Percent = &pct
	}
	return result
}

// ForLearner returns CA for every course the learner is enrolled in.
func (s *CAService) ForLearner(email string) []CourseCA {
	var courses []string
	s.db.Model(&models.StudentEnrollment{}).
		Where("LOWER(email) = ? AND (status IS NULL OR status <> 'rejected')", strings.ToLower(email)).
		Distinct("course").Pluck("course", &courses)
	out := make([]CourseCA, 0, len(courses))
	for _, c := range courses {
		out = append(out, s.ForCourse(email, c))
	}
	return out
}

// EvaluateCertificate awards a certificate when the learner has completed
// every assignment and published quiz in the course with CA ≥ pass mark.
func (s *CAService) EvaluateCertificate(email, course string) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || course == "" {
		return
	}

	var assignmentCount, quizCount int64
	s.db.Model(&models.Assignment{}).Where("course = ?", course).Count(&assignmentCount)
	s.db.Model(&models.Quiz{}).Where("course = ? AND status = 'published'", course).Count(&quizCount)
	if assignmentCount+quizCount == 0 {
		return
	}

	ca := s.ForCourse(email, course)
	var markedAssignments, quizzesDone int
	for _, item := range ca.Items {
		if item.Kind == "assignment" && (item.Status == "marked") {
			markedAssignments++
		}
		if item.Kind == "quiz" {
			quizzesDone++
		}
	}
	if int64(markedAssignments) < assignmentCount || int64(quizzesDone) < quizCount || ca.Percent == nil || *ca.Percent < s.PassMark {
		return
	}

	var existing models.Certificate
	found := s.db.Where("LOWER(learner_email) = ? AND course = ?", email, course).First(&existing).Error == nil
	if found && existing.Status == "earned" && existing.Code != "" {
		return
	}

	code := newCertificateCode()
	detail := fmt.Sprintf("Awarded for completing %s with a continuous assessment score of %.0f%%.", course, *ca.Percent)
	values := map[string]any{
		"status":      "earned",
		"issued_date": time.Now().Format("2006-01-02"),
		"detail":      detail,
		"code":        code,
		"ca_percent":  *ca.Percent,
	}
	if found {
		s.db.Model(&models.Certificate{}).Where("name = ?", existing.Name).Updates(values)
	} else {
		s.db.Create(&models.Certificate{
			Base:         models.Base{Name: models.GenerateName("Certificate")},
			LearnerEmail: email,
			Course:       course,
			Status:       "earned",
			IssuedDate:   values["issued_date"].(string),
			Detail:       detail,
			Code:         code,
			CAPercent:    *ca.Percent,
		})
	}

	s.notifier.Notify([]string{email}, notify.Message{
		Kind:  "certificate",
		Title: "Certificate earned!",
		Body:  fmt.Sprintf("Congratulations — you completed %s (CA %.0f%%). Code %s", course, *ca.Percent, code),
		Link:  "certificates",
		SMS:   true,
	})
}

// newCertificateCode returns a code like ZET-7KQ2-9FXM (no look-alike characters).
func newCertificateCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	pick := func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			b.WriteByte(alphabet[idx.Int64()])
		}
		return b.String()
	}
	return "ZET-" + pick(4) + "-" + pick(4)
}
