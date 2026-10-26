// Command promote-admin flips an existing learner account's Role to "Admin"
// so it can sign in through /portal/authenticate-admin and use /admin/api/*.
// There is no self-service admin signup — this is the only way to create one.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"strings"

	"gorm.io/gorm"

	"zetrc-backend/internal/config"
	"zetrc-backend/internal/db"
	"zetrc-backend/internal/models"
)

func main() {
	email := flag.String("email", "", "email address of the learner account to promote to Admin")
	flag.Parse()

	if strings.TrimSpace(*email) == "" {
		log.Fatal("usage: promote-admin -email you@example.com")
	}

	cfg := config.Load()

	gdb, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("could not connect to database: %v", err)
	}

	var learner models.Learner
	err = gdb.Where("email = ?", strings.ToLower(strings.TrimSpace(*email))).First(&learner).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		log.Fatalf("no learner account found for %q — register it first, then promote it.", *email)
	}
	if err != nil {
		log.Fatalf("could not look up learner: %v", err)
	}

	if learner.Role == "Admin" {
		fmt.Printf("%s is already an Admin.\n", learner.Email)
		return
	}

	if err := gdb.Model(&learner).Update("role", "Admin").Error; err != nil {
		log.Fatalf("could not update role: %v", err)
	}

	fmt.Printf("%s promoted to Admin.\n", learner.Email)
}
