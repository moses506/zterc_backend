// Command seed populates the database with a small, curated set of sample
// ZETRC courses and lecturer accounts, and assigns each lecturer to courses.
// Safe to re-run: existing courses and accounts (matched by name/email) are
// left untouched, and a course that already has a lecturer keeps them.
package main

import (
	"log"
	"time"

	"gorm.io/gorm"

	"zetrc-backend/internal/auth"
	"zetrc-backend/internal/config"
	"zetrc-backend/internal/db"
	"zetrc-backend/internal/models"
)

func seedCourses() []models.Course {
	entries := []struct {
		name        string
		duration    string
		description string
	}{
		{
			"Climate-Smart Agriculture 101",
			"8 weeks",
			"Foundations of climate-resilient farming: crop diversification, drought-tolerant varieties, and adapting planting calendars to shifting rainfall patterns.",
		},
		{
			"Conservation Farming Practices",
			"6 weeks",
			"Minimum tillage, crop rotation, and residue retention techniques that protect soil structure and boost long-term yields.",
		},
		{
			"Agroforestry & Land Restoration",
			"10 weeks",
			"Integrating trees with crops and livestock to restore degraded land, improve soil fertility, and diversify household income.",
		},
		{
			"Soil Health & Fertility Management",
			"6 weeks",
			"Soil testing, nutrient cycling, and fertility-building strategies tailored to Zambia's major soil types.",
		},
		{
			"Climate Change Adaptation for Cooperatives",
			"9 weeks",
			"Helping farmer cooperatives build adaptation plans, access climate financing, and coordinate collective resilience projects.",
		},
	}

	courses := make([]models.Course, 0, len(entries))
	for _, e := range entries {
		courses = append(courses, models.Course{
			Base:        models.Base{Name: e.name},
			Duration:    e.duration,
			Description: e.description,
		})
	}
	return courses
}

// Shared development password for every seeded lecturer account.
const lecturerPassword = "Lecturer@123"

type seedLecturer struct {
	firstName string
	lastName  string
	email     string
	contactNo string
	courses   []string
}

func seedLecturers() []seedLecturer {
	return []seedLecturer{
		{
			firstName: "Mwila",
			lastName:  "Banda",
			email:     "mwila.banda@zetrc.local",
			contactNo: "0971234501",
			courses:   []string{"Climate-Smart Agriculture 101", "Climate Change Adaptation for Cooperatives"},
		},
		{
			firstName: "Natasha",
			lastName:  "Phiri",
			email:     "natasha.phiri@zetrc.local",
			contactNo: "0971234502",
			courses:   []string{"Soil Health & Fertility Management", "Conservation Farming Practices"},
		},
		{
			firstName: "Chilufya",
			lastName:  "Mulenga",
			email:     "chilufya.mulenga@zetrc.local",
			contactNo: "0971234503",
			courses:   []string{"Agroforestry & Land Restoration"},
		},
	}
}

func seedLecturerAccounts(gdb *gorm.DB) {
	hash, err := auth.HashPassword(lecturerPassword)
	if err != nil {
		log.Fatalf("could not hash lecturer password: %v", err)
	}

	for _, l := range seedLecturers() {
		var existing models.Learner
		err := gdb.Where("email = ?", l.email).First(&existing).Error
		switch {
		case err == nil:
			log.Printf("lecturer %s already exists (role %s), skipping account", l.email, existing.Role)
		case err == gorm.ErrRecordNotFound:
			account := models.Learner{
				FirstName:     l.firstName,
				LastName:      l.lastName,
				Email:         l.email,
				ContactNo:     l.contactNo,
				PasswordHash:  hash,
				Role:          models.RoleLecturer,
				Active:        true,
				EmailVerified: true,
			}
			if err := gdb.Create(&account).Error; err != nil {
				log.Printf("failed to create lecturer %s: %v", l.email, err)
				continue
			}
			log.Printf("created lecturer %s %s <%s>", l.firstName, l.lastName, l.email)
		default:
			log.Printf("failed to check lecturer %s: %v", l.email, err)
			continue
		}

		// Assign courses that don't have a lecturer yet.
		result := gdb.Model(&models.Course{}).
			Where("name IN ? AND (lecturer IS NULL OR lecturer = '')", l.courses).
			Update("lecturer", l.email)
		if result.Error != nil {
			log.Printf("failed to assign courses to %s: %v", l.email, result.Error)
		} else if result.RowsAffected > 0 {
			log.Printf("assigned %d course(s) to %s", result.RowsAffected, l.email)
		}
	}
}

type seedAssignment struct {
	title       string
	course      string
	dueInDays   int
	description string
	remarks     string
}

func seedAssignments() []seedAssignment {
	return []seedAssignment{
		// Climate-Smart Agriculture 101
		{"Farm Climate Risk Map", "Climate-Smart Agriculture 101", 7,
			"Sketch a simple map of your farm or plot and mark the areas most affected by drought, flooding or heat. Add one sentence per area explaining what you have observed.",
			"A photo of a hand-drawn map is fine."},
		{"Drought-Tolerant Variety Plan", "Climate-Smart Agriculture 101", 14,
			"Choose two drought-tolerant crop varieties suited to your district. Explain why you chose them and when you would plant them this season.",
			"Include where you would get the seed."},

		// Conservation Farming Practices
		{"Minimum Tillage Field Log", "Conservation Farming Practices", 10,
			"Keep a one-week log of a plot prepared with minimum tillage (basins or ripping). Record labour time, soil moisture observations and any challenges.",
			"Short daily notes are enough."},
		{"Crop Rotation Plan (3 Seasons)", "Conservation Farming Practices", 21,
			"Design a three-season rotation for one field that includes a legume. Show which crop goes in each season and why.",
			""},

		// Agroforestry & Land Restoration
		{"Tree Species Selection Report", "Agroforestry & Land Restoration", 9,
			"Identify three tree species that could be planted with your crops or livestock. For each, describe one benefit (e.g. fodder, soil fertility, shade) and one risk.",
			""},
		{"Degraded Land Restoration Proposal", "Agroforestry & Land Restoration", 24,
			"Pick a degraded piece of land in your community and write a one-page restoration proposal: the problem, the trees and practices you would use, and a simple timeline.",
			"Photos of the site are encouraged."},

		// Soil Health & Fertility Management
		{"Simple Soil Test Report", "Soil Health & Fertility Management", 6,
			"Carry out the jar test and the squeeze test on soil from two fields. Record the results and say what they tell you about soil texture and drainage.",
			"Follow the steps from the 'Reading Your Soil' module."},
		{"Compost Making Journal", "Soil Health & Fertility Management", 18,
			"Start a compost heap and keep a journal for two weeks: materials used, layering, watering and temperature checks. Finish with what you would do differently.",
			""},
		{"Fertilizer Budget for One Hectare", "Soil Health & Fertility Management", 28,
			"Using your soil test results, work out a basic fertilizer plan and cost for one hectare of maize, combining organic and mineral sources.",
			"Show your calculations."},

		// Climate Change Adaptation for Cooperatives
		{"Cooperative Adaptation Action Plan", "Climate Change Adaptation for Cooperatives", 12,
			"With your cooperative, agree on three adaptation actions for the next season. For each, list who is responsible, the resources needed and how you will measure success.",
			"Group submissions are allowed — list all members."},
		{"Climate Finance Opportunities Brief", "Climate Change Adaptation for Cooperatives", 20,
			"Research two sources of climate finance or grants available to Zambian cooperatives. Summarise eligibility, amounts and how to apply.",
			""},
	}
}

func seedAssignmentRecords(gdb *gorm.DB) {
	created, skipped := 0, 0
	for _, a := range seedAssignments() {
		var count int64
		gdb.Model(&models.Assignment{}).Where("name = ?", a.title).Count(&count)
		if count > 0 {
			skipped++
			continue
		}

		var course models.Course
		if gdb.Where("name = ?", a.course).First(&course).Error != nil {
			log.Printf("skipping assignment %q: course %q not found", a.title, a.course)
			continue
		}

		assignment := models.Assignment{
			Base:           models.Base{Name: a.title},
			Course:         a.course,
			SubmissionDate: time.Now().AddDate(0, 0, a.dueInDays).Format("2006-01-02"),
			Description:    a.description,
			Remarks:        a.remarks,
			Status:         "open",
		}

		// Show the course's real lecturer on the assignment.
		if course.Lecturer != "" {
			var lecturer models.Learner
			if gdb.Where("LOWER(email) = LOWER(?)", course.Lecturer).First(&lecturer).Error == nil {
				assignment.LecturerName = lecturer.FullName()
				assignment.ContactNo = lecturer.Email
				assignment.Owner = lecturer.Email
			}
		}

		if err := gdb.Create(&assignment).Error; err != nil {
			log.Printf("failed to seed assignment %q: %v", a.title, err)
			continue
		}
		created++
	}
	log.Printf("assignments: %d created, %d already existed", created, skipped)
}

type seedQuiz struct {
	title, course string
	questions     []models.QuizQuestion
}

func q(prompt string, answer int, options ...string) models.QuizQuestion {
	return models.QuizQuestion{Prompt: prompt, Options: options, Answer: answer, Marks: 1}
}

func seedQuizzes() []seedQuiz {
	return []seedQuiz{
		{"Climate Risk Basics Check", "Climate Change Adaptation for Cooperatives", []models.QuizQuestion{
			q("Which of these is an adaptation action rather than a mitigation action?", 1,
				"Planting trees to absorb carbon", "Switching to drought-tolerant seed varieties", "Reducing fuel use on the farm"),
			q("A cooperative adaptation plan should first identify…", 0,
				"The climate risks members face", "The price of fertilizer", "The number of tractors available"),
			q("Which weather change is most common across Zambia's farming regions?", 2,
				"More snowfall", "Longer cold seasons", "Shorter, less predictable rainy seasons"),
			q("Collective action helps cooperatives because it…", 1,
				"Removes all climate risk", "Shares costs and knowledge among members", "Guarantees higher prices"),
		}},
		{"Soil Health Quick Quiz", "Soil Health & Fertility Management", []models.QuizQuestion{
			q("In the jar test, the layer that settles first (at the bottom) is…", 0, "Sand", "Silt", "Clay"),
			q("Which practice builds soil organic matter?", 2, "Burning crop residues", "Deep ploughing every season", "Adding compost and mulch"),
			q("A soil that stays sticky when squeezed has a lot of…", 1, "Sand", "Clay", "Gravel"),
			q("Legumes in a rotation improve soil mainly by…", 0, "Fixing nitrogen", "Raising soil temperature", "Removing weeds"),
		}},
		{"Climate-Smart Farming Check-in", "Climate-Smart Agriculture 101", []models.QuizQuestion{
			q("Crop diversification helps farmers by…", 1,
				"Making all crops ripen at once", "Spreading risk if one crop fails", "Using more water"),
			q("Planting basins mainly help to…", 0, "Capture and hold rainwater", "Scare birds away", "Speed up harvest"),
			q("The best source of planting-date advice this season is…", 2,
				"Last decade's calendar", "Guesswork", "Local seasonal forecasts and extension officers"),
		}},
		{"Drought-Tolerant Crops Quiz", "Climate-Smart Agriculture 101", []models.QuizQuestion{
			q("Which crop is generally the most drought-tolerant?", 2, "Rice", "Irrigated vegetables", "Sorghum"),
			q("Early-maturing maize varieties help farmers because they…", 0,
				"Finish growing before the rains stop", "Need more fertilizer", "Grow taller"),
			q("Mulching the soil surface mainly…", 1, "Attracts pests", "Keeps moisture in the soil", "Makes the soil acidic"),
			q("Intercropping maize with cowpeas can…", 2,
				"Only increase weeds", "Stop maize from growing", "Improve soil nitrogen and spread risk"),
			q("A farmer who plants the same crop on the same field every year risks…", 0,
				"Build-up of pests and soil exhaustion", "Higher yields every year", "Fewer weeds"),
		}},

		{"Conservation Farming Basics", "Conservation Farming Practices", []models.QuizQuestion{
			q("The three principles of conservation farming are minimum tillage, crop rotation and…", 1,
				"Burning residues", "Permanent soil cover", "Deep ploughing"),
			q("Planting basins are best dug…", 0,
				"In the dry season before the rains", "After the first harvest", "Only when it is raining"),
			q("Why leave crop residues on the field instead of burning them?", 2,
				"They attract more buyers", "They make ploughing easier", "They protect the soil and add organic matter"),
			q("Ripping (with an ox-drawn ripper) is a form of…", 1, "Irrigation", "Minimum tillage", "Pest control"),
		}},
		{"Crop Rotation Check", "Conservation Farming Practices", []models.QuizQuestion{
			q("A good rotation for a maize field includes…", 0, "A legume such as groundnuts or soya", "Maize again", "Leaving the field bare"),
			q("Rotating crops helps break the cycle of…", 2, "Rainfall", "Sunlight", "Pests and diseases"),
			q("Which crop adds nitrogen to the soil?", 1, "Cassava", "Soya beans", "Maize"),
		}},

		{"Agroforestry Essentials", "Agroforestry & Land Restoration", []models.QuizQuestion{
			q("Faidherbia albida is valued in Zambian fields because it…", 0,
				"Drops its leaves in the rainy season and fertilises crops", "Shades out all crops", "Needs no water at all"),
			q("Which is a benefit of planting trees on farmland?", 1, "More soil erosion", "Fodder, fuel and improved soil", "Lower crop yields always"),
			q("Planting trees along contours helps to…", 2, "Speed up water run-off", "Attract termites", "Reduce soil erosion"),
			q("Gliricidia is commonly used in agroforestry to…", 0,
				"Add nitrogen and provide green manure", "Produce timber only", "Repel rain"),
		}},
		{"Land Restoration Quiz", "Agroforestry & Land Restoration", []models.QuizQuestion{
			q("The first step in restoring degraded land is to…", 1,
				"Plant any tree quickly", "Find out why it became degraded", "Burn the remaining vegetation"),
			q("Which practice helps restore overgrazed land?", 2, "Adding more livestock", "Clearing all shrubs", "Rotational grazing and resting areas"),
			q("Farmer-managed natural regeneration means…", 0,
				"Protecting and pruning tree stumps that regrow naturally", "Buying seedlings every year", "Cutting all regrowth"),
		}},

		{"Compost and Organic Matter Quiz", "Soil Health & Fertility Management", []models.QuizQuestion{
			q("A healthy compost heap should feel…", 1, "Completely dry", "Warm and damp like a squeezed sponge", "Waterlogged"),
			q("Which material is a good 'green' for compost?", 0, "Fresh leaves and manure", "Plastic bags", "Metal cans"),
			q("Turning a compost heap regularly…", 2, "Kills the compost", "Is never needed", "Adds air and speeds up decomposition"),
			q("Organic matter improves sandy soils by…", 1, "Making them drain faster", "Helping them hold water and nutrients", "Making them saltier"),
		}},

		{"Cooperative Planning Quiz", "Climate Change Adaptation for Cooperatives", []models.QuizQuestion{
			q("A cooperative seed bank helps members by…", 0,
				"Keeping local seed available after a bad season", "Raising seed prices", "Replacing all farming"),
			q("Weather-index insurance pays out when…", 2,
				"A member forgets to plant", "Prices fall", "Measured rainfall is below an agreed level"),
			q("Which is a good indicator to monitor an adaptation action?", 1,
				"The colour of the office", "Number of members using drought-tolerant seed", "The chairperson's age"),
			q("Village savings groups support adaptation by…", 0,
				"Giving members money to invest in resilient practices", "Stopping farmers from borrowing", "Replacing extension officers"),
		}},
	}
}

func seedQuizRecords(gdb *gorm.DB) {
	created := 0
	for _, sq := range seedQuizzes() {
		var count int64
		gdb.Model(&models.Quiz{}).Where("title = ? AND course = ?", sq.title, sq.course).Count(&count)
		if count > 0 {
			continue
		}
		if gdb.Where("name = ?", sq.course).First(&models.Course{}).Error != nil {
			continue
		}
		gdb.Create(&models.Quiz{
			Base:        models.Base{Name: models.GenerateName("Quiz")},
			Course:      sq.course,
			Title:       sq.title,
			Description: "Choose the best answer for each question. Your best attempt counts towards your CA.",
			Questions:   sq.questions,
			Status:      "published",
			MaxAttempts: 2,
		})
		created++
	}
	log.Printf("quizzes: %d created", created)
}

func main() {
	cfg := config.Load()

	gdb, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("could not connect to database: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		log.Fatalf("could not migrate database: %v", err)
	}

	created := 0
	skipped := 0

	for _, course := range seedCourses() {
		var count int64
		if err := gdb.Model(&models.Course{}).Where("name = ?", course.Name).Count(&count).Error; err != nil {
			log.Printf("failed to check course %q: %v", course.Name, err)
			continue
		}
		if count > 0 {
			skipped++
			continue
		}

		if err := gdb.Create(&course).Error; err != nil {
			log.Printf("failed to seed course %q: %v", course.Name, err)
			continue
		}
		created++
	}

	log.Printf("courses: %d created, %d already existed", created, skipped)

	seedLecturerAccounts(gdb)
	seedAssignmentRecords(gdb)
	seedQuizRecords(gdb)
	log.Printf("seed complete — lecturer password: %s", lecturerPassword)
}
