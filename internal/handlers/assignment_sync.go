package handlers

import (
	"log"
	"strings"

	"gorm.io/gorm"

	"zetrc-backend/internal/models"
)

// syncAssignmentLecturers copies each course's assigned lecturer onto its
// assignments (name, contact email, owner) so learners see the real lecturer.
// An empty course syncs every course. Courses without a lecturer are left as-is.
func syncAssignmentLecturers(db *gorm.DB, course string) {
	query := db.Model(&models.Course{}).Where("lecturer <> ''")
	if course = strings.TrimSpace(course); course != "" {
		query = query.Where("name = ?", course)
	}

	var courses []models.Course
	if err := query.Find(&courses).Error; err != nil {
		log.Printf("assignment lecturer sync: %v", err)
		return
	}

	for _, c := range courses {
		var lecturer models.Learner
		if db.Where("LOWER(email) = LOWER(?)", c.Lecturer).First(&lecturer).Error != nil {
			continue
		}
		db.Model(&models.Assignment{}).Where("course = ?", c.Name).Updates(map[string]any{
			"lecturer_name": lecturer.FullName(),
			"contact_no":    lecturer.Email,
			"owner":         lecturer.Email,
		})
	}
}

// syncAfterWrite keeps assignment lecturer details current after a Course or
// Assignment is created or changed through the generic CRUD endpoints.
func syncAfterWrite(db *gorm.DB, model string, instance any) {
	switch record := instance.(type) {
	case *models.Course:
		if model == "Course" {
			syncAssignmentLecturers(db, record.Name)
		}
	case *models.Assignment:
		if model == "Assignment" && record.Course != "" {
			syncAssignmentLecturers(db, record.Course)
			db.Where("name = ?", record.Name).First(record)
		}
	}
}
