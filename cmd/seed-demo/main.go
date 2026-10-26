// Command seed-demo fills ZETRC with realistic demo activity for
// presentations and demo videos: learners across districts, enrollments
// (some awaiting approval), marked assignments, quiz attempts, lesson
// progress over the past four weeks, discussions, announcements, field
// photos, notifications and certificates.
//
// Run `go run ./cmd/seed` first (courses, lecturers, assignments, quizzes).
//
//	go run ./cmd/seed-demo          # add demo data (safe to re-run: it resets itself first)
//	go run ./cmd/seed-demo -clear   # remove all demo data
//
// Demo accounts use the @demo.zetrc.local domain. Password for all: Demo@1234
package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"

	"zetrc-backend/internal/auth"
	"zetrc-backend/internal/config"
	"zetrc-backend/internal/db"
	"zetrc-backend/internal/handlers"
	"zetrc-backend/internal/models"
	"zetrc-backend/internal/notify"
)

const (
	domain       = "@demo.zetrc.local"
	demoPassword = "Demo@1234"
	demoPrefix   = "DEMO-"
)

type person struct {
	first, last, gender, district, coop, phone string
	ability                                    float64 // 0..1, drives scores and pace
	courses                                    []string
	pending                                    bool // application awaiting admin review
	inactive                                   bool // stopped engaging (shows as "at risk")
}

const (
	csa  = "Climate-Smart Agriculture 101"
	cfp  = "Conservation Farming Practices"
	afr  = "Agroforestry & Land Restoration"
	soil = "Soil Health & Fertility Management"
	coop = "Climate Change Adaptation for Cooperatives"
)

var people = []person{
	{"Mutale", "Kapembwa", "Female", "Kasama", "Mungwi Women Farmers", "0971100101", 0.92, []string{csa, soil}, false, false},
	{"Bwalya", "Chilufya", "Male", "Mansa", "Luapula Maize Growers", "0971100102", 0.78, []string{csa, cfp}, false, false},
	{"Natasha", "Zulu", "Female", "Chipata", "Eastern Soya Co-op", "0971100103", 0.85, []string{soil, coop}, false, false},
	{"Joseph", "Mumba", "Male", "Kabwe", "Kabwe Youth Agripreneurs", "0971100104", 0.55, []string{csa}, false, true},
	{"Grace", "Lungu", "Female", "Chibombo", "Chibombo Farmers Union", "0971100105", 0.88, []string{coop, afr}, false, false},
	{"Emmanuel", "Tembo", "Male", "Lundazi", "Lundazi Groundnut Club", "0971100106", 0.70, []string{cfp, soil}, false, false},
	{"Ruth", "Sakala", "Female", "Mazabuka", "Mazabuka Horticulture Group", "0971100107", 0.95, []string{csa, coop}, false, false},
	{"Peter", "Mwansa", "Male", "Mpika", "Mpika Agroforestry Network", "0971100108", 0.62, []string{afr}, false, false},
	{"Esther", "Phiri", "Female", "Monze", "Monze Conservation Farmers", "0971100109", 0.80, []string{cfp, csa}, false, false},
	{"Kelvin", "Banda", "Male", "Mumbwa", "Mumbwa Cattle & Crops", "0971100110", 0.45, []string{soil}, false, true},
	{"Mercy", "Nkhoma", "Female", "Petauke", "Petauke Sunflower Co-op", "0971100111", 0.7, []string{coop}, true, false},
	{"Daniel", "Siame", "Male", "Solwezi", "", "0971100112", 0.7, []string{afr}, true, false},
}

func email(p person) string {
	return strings.ToLower(p.first+"."+p.last) + domain
}

func main() {
	clear := flag.Bool("clear", false, "remove demo data")
	flag.Parse()

	// Never send real SMS for demo data.
	os.Setenv("NOTIFY_CHANNELS", "log")

	cfg := config.Load()
	gdb, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("could not connect to database: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		log.Fatalf("could not migrate database: %v", err)
	}

	clearDemo(gdb)
	if *clear {
		log.Printf("demo data removed")
		return
	}

	var courseCount int64
	gdb.Model(&models.Course{}).Count(&courseCount)
	if courseCount == 0 {
		log.Fatalf("no courses found — run `go run ./cmd/seed` first")
	}

	rng := rand.New(rand.NewSource(20260927))
	now := time.Now()
	hash, _ := auth.HashPassword(demoPassword)

	// Demo admin for recording.
	gdb.Create(&models.Learner{
		FirstName: "Demo", LastName: "Administrator", Email: "admin" + domain, ContactNo: "0970000999",
		PasswordHash: hash, Role: models.RoleAdmin, Active: true, EmailVerified: true, SMSOptIn: true,
		CreatedAt: now.AddDate(0, -3, 0),
	})

	caService := handlers.NewCAService(gdb, notify.New(gdb))

	for i, p := range people {
		mail := email(p)
		joined := now.AddDate(0, 0, -(80 - i*6))
		gdb.Create(&models.Learner{
			FirstName: p.first, LastName: p.last, Email: mail, ContactNo: p.phone,
			PasswordHash: hash, Role: models.RoleLearner, Active: true, EmailVerified: true,
			Gender: p.gender, District: p.district, Cooperative: p.coop, SMSOptIn: true,
			CreatedAt: joined,
		})

		for ci, course := range p.courses {
			var c models.Course
			if gdb.Where("name = ?", course).First(&c).Error != nil {
				continue
			}
			status := "approved"
			if p.pending {
				status = "pending"
			}
			enrolledAt := joined.Add(time.Duration(ci*36) * time.Hour)
			if p.pending {
				enrolledAt = now.Add(-time.Duration(5+i) * time.Hour)
			}
			gdb.Create(&models.StudentEnrollment{
				Base:        models.Base{Name: demoPrefix + models.GenerateName("Student_Enrollment"), CreatedAt: enrolledAt},
				FirstName:   p.first, LastName: p.last, Email: mail, Course: course, Duration: c.Duration,
				Description: motivation(p, course), Status: status,
				ReviewedBy:  map[bool]string{true: "", false: "admin" + domain}[p.pending],
			})
			if p.pending {
				continue
			}
			seedProgress(gdb, rng, p, mail, course, now)
			seedAssessments(gdb, rng, p, mail, course, now, i)
			caService.EvaluateCertificate(mail, course)
		}
	}

	seedCommunity(gdb, rng, now)
	seedNotifications(gdb, now)
	seedAudit(gdb, now)

	log.Printf("demo data ready — %d learners (2 awaiting approval). Password for all demo accounts: %s", len(people), demoPassword)
	log.Printf("demo admin: admin%s", domain)
}

func motivation(p person, course string) string {
	if p.coop != "" {
		return fmt.Sprintf("I farm in %s and I'm a member of %s. I want to learn practices from %s that I can share with our group.", p.district, p.coop, course)
	}
	return fmt.Sprintf("I farm in %s and want to improve my yields with what I learn in %s.", p.district, course)
}

// ---------- progress ----------

type lessonRef struct{ name, module string }

func courseLessons(gdb *gorm.DB, course string) []lessonRef {
	var rows []struct{ Name, Module string }
	gdb.Table("lesson_contents").
		Select("lesson_contents.name AS name, lesson_contents.module AS module").
		Joins("JOIN course_modules ON course_modules.name = lesson_contents.module").
		Where("course_modules.course = ?", course).Scan(&rows)
	out := []lessonRef{}
	for _, r := range rows {
		out = append(out, lessonRef{r.Name, r.Module})
	}
	if len(out) == 0 {
		for i := 1; i <= 4; i++ {
			out = append(out, lessonRef{fmt.Sprintf("Lesson %d", i), course})
		}
	}
	return out
}

func seedProgress(gdb *gorm.DB, rng *rand.Rand, p person, mail, course string, now time.Time) {
	lessons := courseLessons(gdb, course)
	pace := 3 + p.ability*9 // percentage points per active day
	stopAfter := 28
	if p.inactive {
		stopAfter = 13 // went quiet two weeks ago
	}
	for li, lesson := range lessons {
		key := fmt.Sprintf("demo::%s::%s", lesson.module, lesson.name)
		startDay := 26 - li*int(8-p.ability*4)
		percent := 0.0
		var lastAt time.Time
		for day := startDay; day >= 0; day-- {
			daysActive := startDay - day
			if daysActive > stopAfter || rng.Float64() < 0.35 {
				continue
			}
			percent = math.Min(100, percent+pace*(0.6+rng.Float64()*0.8))
			at := now.AddDate(0, 0, -day).Add(-time.Duration(rng.Intn(8)+8) * time.Hour)
			gdb.Create(&models.ProgressLog{LearnerEmail: mail, Key: key, Course: course, Percent: math.Round(percent), CreatedAt: at})
			lastAt = at
			if percent >= 100 {
				break
			}
		}
		if percent > 0 {
			gdb.Create(&models.LessonProgress{
				LearnerEmail: mail, Key: key, Module: lesson.module, Course: course,
				Percent: math.Round(percent), Completed: percent >= 100, CreatedAt: lastAt, UpdatedAt: lastAt,
			})
		}
	}
}

// ---------- assignments & quizzes ----------

func seedAssessments(gdb *gorm.DB, rng *rand.Rand, p person, mail, course string, now time.Time, idx int) {
	var assignments []models.Assignment
	gdb.Where("course = ?", course).Order("submission_date").Find(&assignments)
	for ai, a := range assignments {
		if p.inactive && ai > 0 {
			break // at-risk learners only submitted the first task
		}
		submittedAt := now.AddDate(0, 0, -(20 - ai*5 - idx%3))
		score := math.Round(20 * clamp(p.ability+(rng.Float64()-0.5)*0.25, 0.3, 1))
		// Leave the most recent submission of a few learners unmarked so there is marking to demo.
		unmarked := ai == len(assignments)-1 && idx%3 == 0
		sub := models.AssignmentSubmission{
			Base:             models.Base{Name: demoPrefix + models.GenerateName("Assignment_Submission"), CreatedAt: submittedAt},
			Assignment:       a.Name, AssignmentName: a.Name, StudentFirstName: p.first, StudentLastName: p.last,
			Student: mail, Content: answerFor(a.Name, p),
		}
		if unmarked {
			sub.Status = "submitted"
		} else {
			sub.Status, sub.Score, sub.MaxScore = "marked", score, 20
			sub.Feedback = feedback(score)
		}
		gdb.Create(&sub)
	}

	var quizzes []models.Quiz
	gdb.Where("course = ? AND status = 'published'", course).Find(&quizzes)
	for qi, quiz := range quizzes {
		if p.inactive && qi > 0 {
			break
		}
		answers := make([]int, len(quiz.Questions))
		var score, max float64
		for i, question := range quiz.Questions {
			marks := question.Marks
			if marks <= 0 {
				marks = 1
			}
			max += marks
			if rng.Float64() < p.ability+0.05 {
				answers[i] = question.Answer
				score += marks
			} else {
				answers[i] = (question.Answer + 1) % len(question.Options)
			}
		}
		gdb.Create(&models.QuizAttempt{
			Quiz: quiz.Name, QuizTitle: quiz.Title, Course: course, LearnerEmail: mail,
			Answers: answers, Score: score, MaxScore: max, CreatedAt: now.AddDate(0, 0, -(12 - qi*4)),
		})
	}
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func answerFor(assignment string, p person) string {
	return fmt.Sprintf("<p>For <strong>%s</strong> I worked on my plot in %s.</p>"+
		"<ul><li>I observed that the soil dries out quickly after the first rains.</li>"+
		"<li>With my group%s we tried planting basins and mulching on half of the field.</li>"+
		"<li>Next season I will compare yields between the two halves.</li></ul>",
		assignment, p.district, map[bool]string{true: " (" + p.coop + ")", false: ""}[p.coop != ""])
}

func feedback(score float64) string {
	switch {
	case score >= 17:
		return "Excellent work — clear observations and a practical plan. Share this with your cooperative."
	case score >= 13:
		return "Good effort. Add measurements (dates, amounts) to make your observations stronger."
	default:
		return "You're on the right track. Please revisit the module notes and add more detail on what you observed."
	}
}

// ---------- community ----------

func lecturerOf(gdb *gorm.DB, course string) (string, string) {
	var c models.Course
	if gdb.Where("name = ?", course).First(&c).Error != nil || c.Lecturer == "" {
		return "", ""
	}
	var l models.Learner
	if gdb.Where("LOWER(email) = LOWER(?)", c.Lecturer).First(&l).Error != nil {
		return c.Lecturer, c.Lecturer
	}
	return l.Email, l.FullName()
}

func seedCommunity(gdb *gorm.DB, rng *rand.Rand, now time.Time) {
	// Announcements from lecturers.
	for _, a := range []struct{ course, title, body, priority string }{
		{csa, "Field day this Friday", "Join us at the Chibombo demo plot at 09:00 to see planting basins and mulching in action. Bring your field notebook.", "important"},
		{soil, "Soil testing kits available", "Each cooperative can collect one soil testing kit from the ZETRC office this week.", "info"},
		{coop, "Climate finance webinar", "A short online session on accessing climate finance for cooperatives will run next Wednesday at 14:00.", "info"},
	} {
		email, name := lecturerOf(gdb, a.course)
		gdb.Create(&models.Announcement{
			Base:   models.Base{Name: demoPrefix + models.GenerateName("Announcement"), CreatedAt: now.Add(-time.Duration(rng.Intn(60)+6) * time.Hour)},
			Course: a.course, Title: a.title, Body: a.body, Priority: a.priority, AuthorEmail: email, AuthorName: name,
		})
	}

	// Discussion threads with replies.
	threads := []struct {
		course, author, title, body string
		replies                     [][2]string // {author ("lecturer" or learner email), body}
		resolved                    bool
	}{
		{csa, email(people[1]), "When should I plant if the rains start late?",
			"In Mansa the rains were two weeks late last year. Should I still plant my usual maize variety?",
			[][2]string{
				{"lecturer", "Good question. Wait until you have had about 30mm of rain in total, then switch to an early-maturing variety so it finishes before the rains stop."},
				{email(people[6]), "In Mazabuka we did exactly that last season and it worked well."},
			}, true},
		{soil, email(people[2]), "Jar test results look strange",
			"My jar test shows almost no clay layer even after a day. Did I do something wrong?",
			[][2]string{{"lecturer", "That usually means a sandy soil. Add more organic matter — compost and mulch — to help it hold water."}}, true},
		{coop, email(people[4]), "Ideas for a cooperative seed bank?",
			"Our group wants to start a seed bank. How do other cooperatives manage who borrows and returns seed?",
			[][2]string{{email(people[2]), "We keep a simple register and members return 1.5x what they borrow after harvest."}}, false},
		{cfp, email(people[8]), "Ripping vs planting basins",
			"Which is better for a small field without oxen: ripping or basins?", nil, false},
	}
	for ti, t := range threads {
		var author models.Learner
		gdb.Where("email = ?", t.author).First(&author)
		created := now.Add(-time.Duration(70-ti*15) * time.Hour)
		thread := models.DiscussionThread{
			Course: t.course, Title: t.title, Body: t.body, AuthorEmail: author.Email, AuthorName: author.FullName(),
			AuthorRole: models.RoleLearner, ReplyCount: len(t.replies), Resolved: t.resolved,
			LastActivity: created.Add(time.Duration(len(t.replies)*3) * time.Hour), CreatedAt: created,
		}
		gdb.Create(&thread)
		for ri, r := range t.replies {
			replyEmail, replyName, role := r[0], "", models.RoleLearner
			if r[0] == "lecturer" {
				replyEmail, replyName = lecturerOf(gdb, t.course)
				role = models.RoleLecturer
			} else {
				var l models.Learner
				gdb.Where("email = ?", r[0]).First(&l)
				replyName = l.FullName()
			}
			gdb.Create(&models.DiscussionReply{
				ThreadID: thread.ID, Body: r[1], AuthorEmail: replyEmail, AuthorName: replyName, AuthorRole: role,
				CreatedAt: created.Add(time.Duration(ri*3+2) * time.Hour),
			})
		}
	}

	// Field journal photos (generated images, embedded so they work with any upload setup).
	photos := []struct {
		who                  int
		course, caption      string
		lat, lng             float64
		daysAgo              int
		sky, crop, soilColor color.RGBA
	}{
		{0, csa, "Planting basins dug before the rains", -10.21, 31.18, 21, rgb(135, 190, 235), rgb(110, 160, 60), rgb(150, 100, 60)},
		{1, cfp, "Maize at 4 weeks with residue mulch", -11.20, 28.89, 14, rgb(150, 200, 240), rgb(70, 150, 50), rgb(140, 95, 55)},
		{6, csa, "Early-maturing variety trial plot", -15.85, 27.77, 9, rgb(170, 210, 245), rgb(90, 170, 60), rgb(160, 110, 70)},
		{4, afr, "Faidherbia seedlings planted along the contour", -14.60, 28.10, 6, rgb(140, 185, 230), rgb(60, 130, 55), rgb(130, 90, 50)},
		{2, soil, "Compost heap after two weeks", -13.64, 32.65, 4, rgb(160, 205, 240), rgb(100, 120, 50), rgb(110, 75, 45)},
		{8, cfp, "Cowpeas intercropped with maize", -16.28, 27.47, 2, rgb(145, 195, 238), rgb(80, 165, 70), rgb(150, 105, 65)},
	}
	for _, ph := range photos {
		p := people[ph.who]
		lat, lng := ph.lat, ph.lng
		gdb.Create(&models.FieldPhoto{
			LearnerEmail: email(p), LearnerName: p.first + " " + p.last, Course: ph.course, Caption: ph.caption,
			ImageURL: fieldImage(rng, ph.sky, ph.crop, ph.soilColor), Latitude: &lat, Longitude: &lng,
			TakenAt:   now.AddDate(0, 0, -ph.daysAgo).Format(time.RFC3339),
			CreatedAt: now.AddDate(0, 0, -ph.daysAgo),
		})
	}
}

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

// fieldImage draws a simple field scene (sky, horizon, crop rows) as a JPEG data URL.
func fieldImage(rng *rand.Rand, sky, crop, soil color.RGBA) string {
	const w, h = 480, 360
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	horizon := 130 + rng.Intn(30)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var c color.RGBA
			if y < horizon {
				t := float64(y) / float64(horizon)
				c = color.RGBA{uint8(float64(sky.R) * (1 - 0.25*t)), uint8(float64(sky.G) * (1 - 0.1*t)), sky.B, 255}
			} else {
				depth := float64(y-horizon) / float64(h-horizon)
				// Perspective crop rows converging towards the horizon.
				row := math.Mod((float64(x)-w/2)/(0.15+depth)+1000, 60)
				if row < 26*(0.4+depth) {
					c = shade(crop, 0.7+0.5*depth+rng.Float64()*0.08)
				} else {
					c = shade(soil, 0.8+0.3*depth+rng.Float64()*0.06)
				}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 72})
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func shade(c color.RGBA, f float64) color.RGBA {
	s := func(v uint8) uint8 { return uint8(clamp(float64(v)*f, 0, 255)) }
	return color.RGBA{s(c.R), s(c.G), s(c.B), 255}
}

// ---------- notifications & audit ----------

func seedNotifications(gdb *gorm.DB, now time.Time) {
	for _, p := range people[:9] {
		if p.pending {
			continue
		}
		gdb.Create(&models.Notification{
			RecipientEmail: email(p), Kind: "announcement", Title: "Field day this Friday",
			Body: "Join us at the Chibombo demo plot at 09:00.", Link: "dashboard", CreatedAt: now.Add(-20 * time.Hour),
		})
	}
	// Lecturers see new questions.
	for _, course := range []string{cfp, coop} {
		if lecturer, _ := lecturerOf(gdb, course); lecturer != "" {
			gdb.Create(&models.Notification{
				RecipientEmail: strings.ToLower(lecturer), Kind: "discussion", Title: "New question in " + course,
				Body: "A learner asked a question in the discussion board.", Link: "discussions", CreatedAt: now.Add(-9 * time.Hour),
			})
		}
	}
}

func seedAudit(gdb *gorm.DB, now time.Time) {
	entries := []models.AuditLog{
		{ActorEmail: "admin" + domain, ActorRole: models.RoleAdmin, Action: "create", Model: "Course", Record: demoPrefix + csa, Summary: "Created Course " + csa},
		{ActorEmail: "admin" + domain, ActorRole: models.RoleAdmin, Action: "update", Model: "Course", Record: demoPrefix + soil, Summary: "Updated Course " + soil + " (lecturer)"},
		{ActorEmail: "admin" + domain, ActorRole: models.RoleAdmin, Action: "review", Model: "Student_Enrollment", Record: demoPrefix + "enrollment", Summary: "Review Student Enrollment (approved)"},
		{ActorEmail: "mwila.banda@zetrc.local", ActorRole: models.RoleLecturer, Action: "update", Model: "Assignment_Submission", Record: demoPrefix + "submission", Summary: "Updated Assignment Submission (status, score, feedback)"},
		{ActorEmail: "mwila.banda@zetrc.local", ActorRole: models.RoleLecturer, Action: "create", Model: "Announcement", Record: demoPrefix + "announcement", Summary: "Created Announcement Field day this Friday"},
	}
	for i, e := range entries {
		e.Changes = "{}"
		e.CreatedAt = now.Add(-time.Duration(48-i*9) * time.Hour)
		gdb.Create(&e)
	}
}

// ---------- reset ----------

func clearDemo(gdb *gorm.DB) {
	like := "%" + domain
	var threadIDs []uint
	gdb.Model(&models.DiscussionThread{}).Where("author_email LIKE ?", like).Pluck("id", &threadIDs)
	if len(threadIDs) > 0 {
		gdb.Where("thread_id IN ?", threadIDs).Delete(&models.DiscussionReply{})
		gdb.Where("id IN ?", threadIDs).Delete(&models.DiscussionThread{})
	}
	gdb.Where("email LIKE ?", like).Delete(&models.StudentEnrollment{})
	gdb.Where("student LIKE ?", like).Delete(&models.AssignmentSubmission{})
	gdb.Where("learner_email LIKE ?", like).Delete(&models.QuizAttempt{})
	gdb.Where("learner_email LIKE ?", like).Delete(&models.LessonProgress{})
	gdb.Where("learner_email LIKE ?", like).Delete(&models.ProgressLog{})
	gdb.Where("learner_email LIKE ?", like).Delete(&models.Certificate{})
	gdb.Where("learner_email LIKE ?", like).Delete(&models.FieldPhoto{})
	gdb.Where("recipient_email LIKE ?", like).Delete(&models.Notification{})
	gdb.Where("name LIKE ?", demoPrefix+"%").Delete(&models.Announcement{})
	gdb.Where("record LIKE ?", demoPrefix+"%").Delete(&models.AuditLog{})
	gdb.Where("kind = 'discussion' AND body = ?", "A learner asked a question in the discussion board.").Delete(&models.Notification{})
	gdb.Where("email LIKE ?", like).Delete(&models.Learner{})
}
