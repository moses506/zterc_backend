package db

import (
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"zetrc-backend/internal/models"
)

func Connect(dsn string) (*gorm.DB, error) {
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	return gdb, nil
}

// Migrate creates/updates tables for every model the backend knows about.
func Migrate(gdb *gorm.DB) error {
	targets := []any{
		&models.Learner{},
		&models.OTP{},
		&models.LessonProgress{},
		&models.ProgressLog{},
	}
	targets = append(targets, models.FeatureModels()...)
	targets = append(targets, models.AllModels()...)

	if err := gdb.AutoMigrate(targets...); err != nil {
		return err
	}

	log.Printf("migrated %d tables", len(targets))
	return nil
}
