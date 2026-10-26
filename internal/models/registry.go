package models

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
)

// registry maps the "model" string sent by the frontend (e.g. "Course",
// "Proposal_Request") to the Go struct type backing it. This lets the
// generic /api/* CRUD handlers work identically for every model without
// per-model handler code.
var registry = map[string]reflect.Type{
	"Course":                reflect.TypeOf(Course{}),
	"Course_Module":         reflect.TypeOf(CourseModule{}),
	"Student_Enrollment":    reflect.TypeOf(StudentEnrollment{}),
	"Lesson_Content":        reflect.TypeOf(LessonContent{}),
	"Assignment":            reflect.TypeOf(Assignment{}),
	"Assignment_Submission": reflect.TypeOf(AssignmentSubmission{}),
	"Lead":                  reflect.TypeOf(Lead{}),
	"Inquiry":               reflect.TypeOf(Inquiry{}),
	"Proposal_Request":      reflect.TypeOf(ProposalRequest{}),
	"Certificate":           reflect.TypeOf(Certificate{}),
	"Announcement":          reflect.TypeOf(Announcement{}),
	"Quiz":                  reflect.TypeOf(Quiz{}),
}

// AllModels returns a new zero-value instance of every registered model, for
// use with GORM's AutoMigrate at startup.
func AllModels() []any {
	out := make([]any, 0, len(registry))
	for _, t := range registry {
		out = append(out, reflect.New(t).Interface())
	}
	return out
}

// NewInstance returns a pointer to a fresh zero-value struct for modelName,
// e.g. NewInstance("Course") -> *Course. The second return value is false if
// modelName is not registered.
func NewInstance(modelName string) (any, bool) {
	t, ok := registry[modelName]
	if !ok {
		return nil, false
	}
	return reflect.New(t).Interface(), true
}

// NewSlice returns a pointer to an empty slice of modelName's struct type,
// e.g. NewSlice("Course") -> *[]Course, suitable for gorm.Find.
func NewSlice(modelName string) (any, bool) {
	t, ok := registry[modelName]
	if !ok {
		return nil, false
	}
	sliceType := reflect.SliceOf(t)
	slicePtr := reflect.New(sliceType)
	return slicePtr.Interface(), true
}

// IsRegistered reports whether modelName is a known generic CRUD model.
func IsRegistered(modelName string) bool {
	_, ok := registry[modelName]
	return ok
}

// KnownModelNames lists every registered model name, sorted for stable output.
func KnownModelNames() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// NamePrefix derives the auto-naming series prefix for a model, e.g.
// "Student_Enrollment" -> "STUDENT-ENROLLMENT", "Course" -> "COURSE".
func NamePrefix(modelName string) string {
	upper := strings.ToUpper(modelName)
	return strings.ReplaceAll(upper, "_", "-")
}

// GenerateName builds a naming-series style identifier with a random suffix,
// e.g. "COURSE-4f3a9b1c2d". Random rather than sequential to avoid needing a
// row-count query (and its race condition) on every create.
func GenerateName(modelName string) string {
	buf := make([]byte, 5)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%s-%s", NamePrefix(modelName), hex.EncodeToString(buf))
}
