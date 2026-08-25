package middleware

import (
	"net/http"
	"os"
	"portfolio/internal/services"
)

func RequireAdminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("admin_token")

		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		email, err := services.ValidateJWT(cookie.Value)
		if err != nil || email != os.Getenv("ADMIN_GMAIL") {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r)
	})
}
