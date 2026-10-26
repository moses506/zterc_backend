package main

import (
	"log"
	"net/http"

	"zetrc-backend/internal/auth"
	"zetrc-backend/internal/config"
	"zetrc-backend/internal/db"
	"zetrc-backend/internal/handlers"
	"zetrc-backend/internal/otp"
)

func main() {
	cfg := config.Load()

	gdb, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("could not connect to database: %v", err)
	}

	if err := db.Migrate(gdb); err != nil {
		log.Fatalf("could not migrate database: %v", err)
	}

	tokens := auth.NewTokenIssuer(cfg.JWTSecret, cfg.JWTTTL)
	otps := otp.NewService(gdb, cfg.OTPTTL)

	router := handlers.NewRouter(gdb, tokens, otps, cfg.AllowedOrigins, cfg.UploadDir, cfg.MaxUploadSizeMB)

	addr := ":" + cfg.Port
	log.Printf("zetrc-backend listening on %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
