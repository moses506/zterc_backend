package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"zetrc-backend/internal/auth"
	"zetrc-backend/internal/httpx"
	"zetrc-backend/internal/models"
)

// UsersHandler lets admins manage accounts: students (Learner), lecturers
// and other admins. Mounted under /admin/api/users.
type UsersHandler struct {
	db *gorm.DB
}

func NewUsersHandler(db *gorm.DB) *UsersHandler {
	return &UsersHandler{db: db}
}

type userPayload struct {
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
	Email     *string `json:"email"`
	ContactNo *string `json:"contact_no"`
	Password  *string `json:"password"`
	Role      *string `json:"role"`
	Active    *bool   `json:"active"`
}

func trimmed(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// GET /admin/api/users?role=Lecturer
func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	query := h.db.Order("created_at DESC")
	if role := r.URL.Query().Get("role"); role != "" {
		if !models.IsValidRole(role) {
			httpx.WriteError(w, http.StatusBadRequest, "Unknown role: "+role)
			return
		}
		query = query.Where("role = ?", role)
	}

	var users []models.Learner
	if err := query.Find(&users).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not load users.")
		return
	}

	httpx.WriteData(w, http.StatusOK, users)
}

// POST /admin/api/users
func (h *UsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req userPayload
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	user := models.Learner{
		FirstName:     trimmed(req.FirstName),
		LastName:      trimmed(req.LastName),
		Email:         strings.ToLower(trimmed(req.Email)),
		ContactNo:     trimmed(req.ContactNo),
		Role:          trimmed(req.Role),
		Active:        true,
		EmailVerified: true, // created by an admin, so no signup OTP step
	}
	if user.Role == "" {
		user.Role = models.RoleLearner
	}

	switch {
	case user.FirstName == "":
		httpx.WriteError(w, http.StatusBadRequest, "First name is required.")
		return
	case !emailPattern.MatchString(user.Email):
		httpx.WriteError(w, http.StatusBadRequest, "A valid email address is required.")
		return
	case !models.IsValidRole(user.Role):
		httpx.WriteError(w, http.StatusBadRequest, "Role must be Admin, Lecturer or Learner.")
		return
	case len(trimmed(req.Password)) < 8:
		httpx.WriteError(w, http.StatusBadRequest, "Password must be at least 8 characters.")
		return
	}

	hash, err := auth.HashPassword(*req.Password)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not create account.")
		return
	}
	user.PasswordHash = hash

	if err := h.db.Create(&user).Error; err != nil {
		httpx.WriteError(w, http.StatusConflict, "An account with this email already exists.")
		return
	}
	recordAudit(h.db, r, "create", "Account", user.Email, map[string]any{"role": user.Role, "name": user.FullName()})

	httpx.WriteData(w, http.StatusCreated, user)
}

func (h *UsersHandler) load(w http.ResponseWriter, r *http.Request) (*models.Learner, bool) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid user id.")
		return nil, false
	}

	var user models.Learner
	if err := h.db.First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "User not found.")
		} else {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not load user.")
		}
		return nil, false
	}
	return &user, true
}

// PATCH /admin/api/users/{id}
func (h *UsersHandler) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := h.load(w, r)
	if !ok {
		return
	}

	var req userPayload
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	isSelf := ClaimsFrom(r) != nil && ClaimsFrom(r).LearnerID == user.ID
	updates := map[string]any{}

	if req.FirstName != nil {
		if trimmed(req.FirstName) == "" {
			httpx.WriteError(w, http.StatusBadRequest, "First name is required.")
			return
		}
		updates["first_name"] = trimmed(req.FirstName)
	}
	if req.LastName != nil {
		updates["last_name"] = trimmed(req.LastName)
	}
	if req.ContactNo != nil {
		updates["contact_no"] = trimmed(req.ContactNo)
	}

	oldEmail := user.Email
	newEmail := oldEmail
	if req.Email != nil {
		newEmail = strings.ToLower(trimmed(req.Email))
		if !emailPattern.MatchString(newEmail) {
			httpx.WriteError(w, http.StatusBadRequest, "A valid email address is required.")
			return
		}
		updates["email"] = newEmail
	}

	if req.Role != nil {
		role := trimmed(req.Role)
		if !models.IsValidRole(role) {
			httpx.WriteError(w, http.StatusBadRequest, "Role must be Admin, Lecturer or Learner.")
			return
		}
		if isSelf && role != models.RoleAdmin {
			httpx.WriteError(w, http.StatusBadRequest, "You cannot remove your own admin access.")
			return
		}
		updates["role"] = role
	}

	if req.Active != nil {
		if isSelf && !*req.Active {
			httpx.WriteError(w, http.StatusBadRequest, "You cannot deactivate your own account.")
			return
		}
		updates["active"] = *req.Active
	}

	if pw := trimmed(req.Password); pw != "" {
		if len(pw) < 8 {
			httpx.WriteError(w, http.StatusBadRequest, "Password must be at least 8 characters.")
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not update password.")
			return
		}
		updates["password_hash"] = hash
	}

	err := h.db.Transaction(func(tx *gorm.DB) error {
		if len(updates) > 0 {
			if err := tx.Model(user).Updates(updates).Error; err != nil {
				return err
			}
		}
		// Keep course assignments pointing at the lecturer's new email.
		if newEmail != oldEmail {
			return tx.Model(&models.Course{}).Where("lecturer = ?", oldEmail).Update("lecturer", newEmail).Error
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, http.StatusConflict, "Could not update user: "+err.Error())
		return
	}

	if err := h.db.First(user, user.ID).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not reload user.")
		return
	}
	if user.Role == models.RoleLecturer {
		syncAssignmentLecturers(h.db, "") // name or email may have changed
	}
	recordAudit(h.db, r, "update", "Account", user.Email, updates)

	httpx.WriteData(w, http.StatusOK, user)
}

// DELETE /admin/api/users/{id}
func (h *UsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := h.load(w, r)
	if !ok {
		return
	}

	if claims := ClaimsFrom(r); claims != nil && claims.LearnerID == user.ID {
		httpx.WriteError(w, http.StatusBadRequest, "You cannot delete your own account.")
		return
	}

	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Course{}).Where("lecturer = ?", user.Email).Update("lecturer", "").Error; err != nil {
			return err
		}
		return tx.Delete(user).Error
	})
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not delete user.")
		return
	}
	recordAudit(h.db, r, "delete", "Account", user.Email, map[string]any{"role": user.Role, "name": user.FullName()})

	httpx.WriteData(w, http.StatusOK, map[string]any{"id": user.ID, "deleted": true})
}
