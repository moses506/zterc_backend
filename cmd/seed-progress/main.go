// Command seed-progress creates sample lesson progress for one learner so the
// Learner Progress chart has a realistic curve to show. Every row it writes
// has a key starting with "demo::", so -clear removes exactly those rows.
//
//	go run ./cmd/seed-progress                         # Chanda Mwale, her enrolled course
//	go run ./cmd/seed-progress -email x@y.com -course "Soil Health & Fertility Management"
//	go run ./cmd/seed-progress -clear                  # remove all sample progress
package main

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"

	"zetrc-backend/internal/config"
	"zetrc-backend/internal/db"
	"zetrc-backend/internal/models"
)

const demoPrefix = "demo::"

// Milestones spread over the last two weeks: {days ago, percent}, per lesson.
var lessonPlans = [][][2]float64{
	{{13, 12}, {12, 35}, {11, 60}, {9, 85}, {8, 100}},
	{{7, 15}, {6, 30}, {4, 55}, {3, 80}},
	{{2, 20}, {1, 35}, {0, 50}},
	{{0, 10}},
}

func main() {
	email := flag.String("email", "chanda.mwale@example.com", "learner email")
	course := flag.String("course", "", "course name (default: the learner's latest enrollment)")
	clear := flag.Bool("clear", false, "remove all sample progress instead of adding it")
	flag.Parse()

	cfg := config.Load()
	gdb, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("could not connect to database: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		log.Fatalf("could not migrate database: %v", err)
	}

	if *clear {
		a := gdb.Where("key LIKE ?", demoPrefix+"%").Delete(&models.LessonProgress{})
		b := gdb.Where("key LIKE ?", demoPrefix+"%").Delete(&models.ProgressLog{})
		log.Printf("removed %d progress rows and %d history points", a.RowsAffected, b.RowsAffected)
		return
	}

	learner := strings.ToLower(strings.TrimSpace(*email))
	courseName := strings.TrimSpace(*course)
	if courseName == "" {
		var enrollment models.StudentEnrollment
		if err := gdb.Where("LOWER(email) = ?", learner).Order("created_at DESC").First(&enrollment).Error; err != nil {
			log.Fatalf("%s has no enrollment; pass -course explicitly", learner)
		}
		courseName = enrollment.Course
	}

	lessons := courseLessons(gdb, courseName)
	log.Printf("seeding progress for %s in %q across %d lesson(s)", learner, courseName, len(lessons))

	now := time.Now()
	logs := 0
	for i, lesson := range lessons {
		plan := lessonPlans[i%len(lessonPlans)]
		if i >= len(lessonPlans) {
			plan = [][2]float64{{0, 5}}
		}
		key := fmt.Sprintf("%s%s::%s", demoPrefix, lesson.module, lesson.name)

		gdb.Where("learner_email = ? AND key = ?", learner, key).Delete(&models.LessonProgress{})
		gdb.Where("learner_email = ? AND key = ?", learner, key).Delete(&models.ProgressLog{})

		final := 0.0
		var lastAt time.Time
		for _, step := range plan {
			at := now.AddDate(0, 0, -int(step[0])).Add(-time.Duration(3+i) * time.Hour)
			if at.After(now) {
				at = now
			}
			gdb.Create(&models.ProgressLog{
				LearnerEmail: learner,
				Key:          key,
				Course:       courseName,
				Percent:      step[1],
				CreatedAt:    at,
			})
			final, lastAt = step[1], at
			logs++
		}

		gdb.Create(&models.LessonProgress{
			LearnerEmail: learner,
			Key:          key,
			Module:       lesson.module,
			Course:       courseName,
			Percent:      final,
			Completed:    final >= 100,
			CreatedAt:    now.AddDate(0, 0, -int(plan[0][0])),
			UpdatedAt:    lastAt,
		})
	}

	log.Printf("done: %d history points written. Remove them later with: go run ./cmd/seed-progress -clear", logs)
}

type lessonRef struct{ name, module string }

// courseLessons lists the course's published lessons, or three placeholders
// when the course has none yet.
func courseLessons(gdb *gorm.DB, course string) []lessonRef {
	var rows []struct {
		Name   string
		Module string
	}
	gdb.Table("lesson_contents").
		Select("lesson_contents.name AS name, lesson_contents.module AS module").
		Joins("JOIN course_modules ON course_modules.name = lesson_contents.module").
		Where("course_modules.course = ?", course).
		Order("course_modules.sort_order, lesson_contents.created_at").
		Scan(&rows)

	lessons := make([]lessonRef, 0, len(rows))
	for _, row := range rows {
		lessons = append(lessons, lessonRef{name: row.Name, module: row.Module})
	}
	if len(lessons) == 0 {
		for i := 1; i <= 3; i++ {
			lessons = append(lessons, lessonRef{name: fmt.Sprintf("Lesson %d", i), module: course})
		}
	}
	return lessons
}
