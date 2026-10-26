package handlers

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"zetrc-backend/internal/auth"
	"zetrc-backend/internal/httpx"
	"zetrc-backend/internal/models"
	"zetrc-backend/internal/otp"
)

type PortalHandler struct {
	db     *gorm.DB
	tokens *auth.TokenIssuer
	otps   *otp.Service
}

func NewPortalHandler(db *gorm.DB, tokens *auth.TokenIssuer, otps *otp.Service) *PortalHandler {
	return &PortalHandler{db: db, tokens: tokens, otps: otps}
}

type signupStage1 struct {
	FirstName            string `json:"first_name"`
	LastName             string `json:"last_name"`
	Email                string `json:"email"`
	ContactNo            string `json:"contact_no"`
	Password             string `json:"password"`
	PasswordConfirmation string `json:"password_confirmation"`
}

type signupRequest struct {
	Stage1 signupStage1 `json:"stage_1"`
}

var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func (s signupStage1) validate(db *gorm.DB, requireUnique bool) (string, bool) {
	if strings.TrimSpace(s.FirstName) == "" {
		return "First name is required.", false
	}
	if strings.TrimSpace(s.LastName) == "" {
		return "Last name is required.", false
	}
	if !emailPattern.MatchString(s.Email) {
		return "A valid email address is required.", false
	}
	if strings.TrimSpace(s.ContactNo) == "" {
		return "Contact number is required.", false
	}
	if len(s.Password) < 8 {
		return "Password must be at least 8 characters.", false
	}
	if s.Password != s.PasswordConfirmation {
		return "Password and confirmation do not match.", false
	}
	if requireUnique {
		var count int64
		db.Model(&models.Learner{}).Where("email = ?", s.Email).Count(&count)
		if count > 0 {
			return "An account with this email already exists.", false
		}
	}
	return "", true
}

// POST /portal/validate-signup-data
func (h *PortalHandler) ValidateSignupData(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	if msg, ok := req.Stage1.validate(h.db, true); !ok {
		httpx.WriteError(w, http.StatusBadRequest, msg)
		return
	}

	httpx.WriteData(w, http.StatusOK, map[string]any{"valid": true})
}

// POST /portal/submit-learner-registration
func (h *PortalHandler) SubmitLearnerRegistration(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	stage := req.Stage1
	if msg, ok := stage.validate(h.db, true); !ok {
		httpx.WriteError(w, http.StatusBadRequest, msg)
		return
	}

	hash, err := auth.HashPassword(stage.Password)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not create account.")
		return
	}

	learner := models.Learner{
		FirstName:    strings.TrimSpace(stage.FirstName),
		LastName:     strings.TrimSpace(stage.LastName),
		Email:        strings.ToLower(strings.TrimSpace(stage.Email)),
		ContactNo:    strings.TrimSpace(stage.ContactNo),
		PasswordHash: hash,
	}

	if err := h.db.Create(&learner).Error; err != nil {
		httpx.WriteError(w, http.StatusConflict, "An account with this email already exists.")
		return
	}

	if _, err := h.otps.Generate(learner.Email, models.OTPPurposeSignup); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Account created but the verification code could not be sent.")
		return
	}

	httpx.WriteData(w, http.StatusCreated, map[string]any{
		"first_name": learner.FirstName,
		"last_name":  learner.LastName,
		"full_name":  learner.FullName(),
		"email":      learner.Email,
		"contact_no": learner.ContactNo,
		"message":    "Registration successful. Please verify your email with the OTP we sent.",
	})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// authenticateByCredentials looks up a learner by email and checks their
// password. Shared by the learner and admin login handlers below.
func (h *PortalHandler) authenticateByCredentials(email, password string) (*models.Learner, error) {
	var learner models.Learner
	err := h.db.Where("email = ?", strings.ToLower(strings.TrimSpace(email))).First(&learner).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err // database problem — don't report it as a wrong password
	}
	if err != nil || !auth.CheckPassword(learner.PasswordHash, password) {
		return nil, errInvalidCredentials
	}
	if !learner.Active {
		return nil, errInactiveAccount
	}
	return &learner, nil
}

var (
	errInvalidCredentials = errors.New("invalid email or password")
	errInactiveAccount    = errors.New("account deactivated")
)

// writeLoginError maps authenticateByCredentials errors to HTTP responses.
func writeLoginError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvalidCredentials):
		httpx.WriteError(w, http.StatusUnauthorized, "Invalid email or password.")
	case errors.Is(err, errInactiveAccount):
		httpx.WriteError(w, http.StatusForbidden, "This account has been deactivated. Please contact the ZETRC administrator.")
	default:
		httpx.WriteError(w, http.StatusInternalServerError, "Could not sign in right now.")
	}
}

// POST /portal/authenticate
func (h *PortalHandler) Authenticate(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	learner, err := h.authenticateByCredentials(req.Email, req.Password)
	if err != nil {
		writeLoginError(w, err)
		return
	}

	token, err := h.tokens.Issue(learner.ID, learner.Email, learner.Role)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not sign in right now.")
		return
	}

	httpx.WriteData(w, http.StatusOK, map[string]any{
		"first_name":     learner.FirstName,
		"last_name":      learner.LastName,
		"full_name":      learner.FullName(),
		"email":          learner.Email,
		"contact_no":     learner.ContactNo,
		"email_verified": learner.EmailVerified,
		"role":           learner.Role,
		"token":          token,
	})
}

// POST /portal/authenticate-admin
func (h *PortalHandler) AuthenticateAdmin(w http.ResponseWriter, r *http.Request) {
	h.authenticateWithRole(w, r, models.RoleAdmin, "This account does not have admin access.")
}

// POST /portal/authenticate-lecturer
func (h *PortalHandler) AuthenticateLecturer(w http.ResponseWriter, r *http.Request) {
	h.authenticateWithRole(w, r, models.RoleLecturer, "This account is not a lecturer account.")
}

func (h *PortalHandler) authenticateWithRole(w http.ResponseWriter, r *http.Request, role, forbidden string) {
	var req loginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	learner, err := h.authenticateByCredentials(req.Email, req.Password)
	if err != nil {
		writeLoginError(w, err)
		return
	}

	if learner.Role != role {
		httpx.WriteError(w, http.StatusForbidden, forbidden)
		return
	}

	token, err := h.tokens.Issue(learner.ID, learner.Email, learner.Role)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not sign in right now.")
		return
	}

	httpx.WriteData(w, http.StatusOK, map[string]any{
		"first_name":     learner.FirstName,
		"last_name":      learner.LastName,
		"full_name":      learner.FullName(),
		"email":          learner.Email,
		"contact_no":     learner.ContactNo,
		"email_verified": learner.EmailVerified,
		"role":           learner.Role,
		"token":          token,
	})
}

type otpEmailRequest struct {
	Email string `json:"email"`
}

// POST /portal/request-otp
func (h *PortalHandler) RequestOTP(w http.ResponseWriter, r *http.Request) {
	var req otpEmailRequest
	if err := httpx.DecodeJSON(r, &req); err != nil || !emailPattern.MatchString(req.Email) {
		httpx.WriteError(w, http.StatusBadRequest, "A valid email address is required.")
		return
	}

	var learner models.Learner
	if err := h.db.Where("email = ?", strings.ToLower(req.Email)).First(&learner).Error; err != nil {
		httpx.WriteError(w, http.StatusNotFound, "No account found for this email.")
		return
	}

	if _, err := h.otps.Generate(learner.Email, models.OTPPurposeLogin); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not send OTP right now.")
		return
	}

	httpx.WriteData(w, http.StatusOK, map[string]any{"message": "OTP sent."})
}

type otpVerifyRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

// POST /portal/validate-otp
func (h *PortalHandler) ValidateOTP(w http.ResponseWriter, r *http.Request) {
	h.verifyOTP(w, r, models.OTPPurposeLogin, "valid")
}

// POST /portal/verify-signup-otp
func (h *PortalHandler) VerifySignupOTP(w http.ResponseWriter, r *http.Request) {
	var req otpVerifyRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	ok, err := h.otps.Verify(strings.ToLower(req.Email), req.OTP, models.OTPPurposeSignup)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not verify OTP right now.")
		return
	}
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid or expired OTP.")
		return
	}

	if err := h.db.Model(&models.Learner{}).Where("email = ?", strings.ToLower(req.Email)).
		Update("email_verified", true).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not verify OTP right now.")
		return
	}

	httpx.WriteData(w, http.StatusOK, map[string]any{"verified": true})
}

func (h *PortalHandler) verifyOTP(w http.ResponseWriter, r *http.Request, purpose models.OTPPurpose, successKey string) {
	var req otpVerifyRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	ok, err := h.otps.Verify(strings.ToLower(req.Email), req.OTP, purpose)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not verify OTP right now.")
		return
	}
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid or expired OTP.")
		return
	}

	httpx.WriteData(w, http.StatusOK, map[string]any{successKey: true})
}
