package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"gorm.io/gorm"

	"zetrc-backend/internal/auth"
	"zetrc-backend/internal/httpx"
	"zetrc-backend/internal/notify"
	"zetrc-backend/internal/otp"
)

func NewRouter(db *gorm.DB, tokens *auth.TokenIssuer, otps *otp.Service, allowedOrigins []string, uploadDir string, maxUploadSizeMB int64) http.Handler {
	r := chi.NewRouter()

	// Bring assignment lecturer details in line with course assignments on startup.
	syncAssignmentLecturers(db, "")

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(httpx.CORS(allowedOrigins))

	r.Get("/health", func(w http.ResponseWriter, req *http.Request) {
		httpx.WriteData(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	notifier := notify.New(db)
	ca := NewCAService(db, notifier)
	crud := NewCRUDHandler(db, notifier, ca)
	upload := NewUploadHandler(uploadDir, maxUploadSizeMB)
	portal := NewPortalHandler(db, tokens, otps)
	progress := NewProgressHandler(db)
	learner := NewLearnerHandler(db, ca)
	quizzes := NewQuizHandler(db, ca)
	community := NewCommunityHandler(db, notifier)
	gradebook := NewGradebookHandler(db, ca)
	impact := NewImpactHandler(db, ca)
	users := NewUsersHandler(db)
	lecturer := NewLecturerHandler(db, crud)
	public := NewPublicAPIHandler(db, crud)

	signedIn := RequireAnyAccount(tokens)

	r.Route("/portal", func(r chi.Router) {
		// Public
		r.Post("/validate-signup-data", portal.ValidateSignupData)
		r.Post("/submit-learner-registration", portal.SubmitLearnerRegistration)
		r.Post("/authenticate", portal.Authenticate)
		r.Post("/authenticate-admin", portal.AuthenticateAdmin)
		r.Post("/authenticate-lecturer", portal.AuthenticateLecturer)
		r.Post("/request-otp", portal.RequestOTP)
		r.Post("/validate-otp", portal.ValidateOTP)
		r.Post("/verify-signup-otp", portal.VerifySignupOTP)
		r.Get("/verify-certificate/{code}", learner.VerifyCertificate)

		// Any signed-in account
		r.Group(func(r chi.Router) {
			r.Use(signedIn)
			r.Post("/lesson-progress", progress.Record)
			r.Get("/my-submissions", learner.MySubmissions)
			r.Get("/my-ca", learner.MyCA)
			r.Get("/my-enrollments", learner.MyEnrollments)
			r.Get("/me", learner.Me)
			r.Patch("/me", learner.UpdateMe)
			r.Get("/notifications", learner.Notifications)
			r.Post("/notifications/read", learner.MarkNotificationsRead)
			r.Get("/announcements", learner.Announcements)

			r.Get("/quizzes", quizzes.List)
			r.Post("/quizzes/{name}/attempt", quizzes.Attempt)

			r.Get("/discussions", community.ListThreads)
			r.Post("/discussions", community.CreateThread)
			r.Get("/discussions/{id}", community.GetThread)
			r.Patch("/discussions/{id}", community.UpdateThread)
			r.Delete("/discussions/{id}", community.DeleteThread)
			r.Post("/discussions/{id}/replies", community.Reply)

			r.Get("/field-photos", community.MyPhotos)
			r.Post("/field-photos", community.AddPhoto)
			r.Delete("/field-photos/{id}", community.DeletePhoto)
		})
	})

	// Public website + learner dashboard data. Reads of course content are
	// open; anything personal needs the learner's login; no edits or deletes.
	r.Route("/api", func(r chi.Router) {
		r.Use(OptionalAuth(tokens))
		r.Get("/get-data/", public.Get)
		r.Get("/get-data", public.Get)
		r.Post("/post-data/", public.Create)
		r.Post("/post-data", public.Create)
		for _, path := range []string{"/patch-data", "/delete-doc", "/submit-doc", "/cancel-doc"} {
			r.HandleFunc(path, public.Blocked)
			r.HandleFunc(path+"/", public.Blocked)
		}
		r.With(signedIn).Post("/upload-file/", upload.Upload)
		r.With(signedIn).Post("/upload-file", upload.Upload)
	})

	r.Route("/admin/api", func(r chi.Router) {
		r.Use(RequireAdmin(tokens))
		r.Get("/users", users.List)
		r.Post("/users", users.Create)
		r.Patch("/users/{id}", users.Update)
		r.Delete("/users/{id}", users.Delete)
		r.Get("/progress", progress.Report)
		r.Get("/gradebook", gradebook.Get)
		r.Get("/impact", impact.Report)
		r.Get("/audit-log", impact.AuditLog)
		r.Post("/enrollments/review", community.ReviewEnrollment)
		r.Get("/field-photos", community.StaffPhotos)
		r.Post("/upload-file/", upload.Upload)
		r.Post("/upload-file", upload.Upload)
		mountCRUD(r, crud.Get, crud.Create, crud.Update, crud.Delete)
		r.Put("/submit-doc/", crud.Submit)
		r.Put("/submit-doc", crud.Submit)
		r.Put("/cancel-doc/", crud.Cancel)
		r.Put("/cancel-doc", crud.Cancel)
	})

	r.Route("/lecturer/api", func(r chi.Router) {
		r.Use(RequireLecturer(tokens))
		r.Get("/progress", progress.Report)
		r.Get("/gradebook", gradebook.Get)
		r.Get("/field-photos", community.StaffPhotos)
		r.Post("/upload-file/", upload.Upload)
		r.Post("/upload-file", upload.Upload)
		mountCRUD(r, lecturer.Get, lecturer.Create, lecturer.Update, lecturer.Delete)
	})

	fileServer := http.FileServer(http.Dir(uploadDir))
	r.Handle("/uploads/*", http.StripPrefix("/uploads/", fileServer))

	return r
}

// mountCRUD registers the generic get/post/patch/delete endpoints (with and
// without trailing slash, as the frontend uses both).
func mountCRUD(r chi.Router, get, create, update, remove http.HandlerFunc) {
	for _, suffix := range []string{"", "/"} {
		r.Get("/get-data"+suffix, get)
		r.Post("/post-data"+suffix, create)
		r.Patch("/patch-data"+suffix, update)
		r.Delete("/delete-doc"+suffix, remove)
	}
}
