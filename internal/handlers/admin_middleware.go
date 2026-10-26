package handlers

import (
	"context"
	"net/http"
	"strings"

	"zetrc-backend/internal/auth"
	"zetrc-backend/internal/httpx"
	"zetrc-backend/internal/models"
)

type claimsKey struct{}

// ClaimsFrom returns the JWT claims stored on the request by RequireRole.
func ClaimsFrom(r *http.Request) *auth.Claims {
	claims, _ := r.Context().Value(claimsKey{}).(*auth.Claims)
	return claims
}

// RequireRole gates a route group behind a valid JWT whose Role is one of
// roles. The parsed claims are stored on the request context so handlers can
// scope data to the signed-in account.
func RequireRole(tokens *auth.TokenIssuer, roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			token, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || token == "" {
				httpx.WriteError(w, http.StatusUnauthorized, "Missing or invalid authorization header.")
				return
			}

			claims, err := tokens.Parse(token)
			if err != nil {
				httpx.WriteError(w, http.StatusUnauthorized, "Invalid or expired session.")
				return
			}

			if !allowed[claims.Role] {
				httpx.WriteError(w, http.StatusForbidden, "You do not have access to this area.")
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin gates a route group behind an Admin JWT.
func RequireAdmin(tokens *auth.TokenIssuer) func(http.Handler) http.Handler {
	return RequireRole(tokens, models.RoleAdmin)
}

// RequireLecturer gates a route group behind a Lecturer JWT.
func RequireLecturer(tokens *auth.TokenIssuer) func(http.Handler) http.Handler {
	return RequireRole(tokens, models.RoleLecturer)
}

// OptionalAuth parses a Bearer token when one is sent and stores its claims,
// but lets anonymous requests through. Handlers decide what needs a login.
func OptionalAuth(tokens *auth.TokenIssuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && token != "" {
				if claims, err := tokens.Parse(token); err == nil {
					r = r.WithContext(context.WithValue(r.Context(), claimsKey{}, claims))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyAccount gates a route behind any valid session (learner or staff).
func RequireAnyAccount(tokens *auth.TokenIssuer) func(http.Handler) http.Handler {
	return RequireRole(tokens, models.RoleLearner, models.RoleLecturer, models.RoleAdmin)
}
