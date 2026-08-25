package handlers

import (
	"fmt"
	"net/http"
	"os"

	"portfolio/cmd/templates"
	"portfolio/internal/services"

	"github.com/a-h/templ"
)

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("admin_token")
	if err == nil && cookie.Value != "" {
		email, err := services.ValidateJWT(cookie.Value)
		if err == nil && email == os.Getenv("ADMIN_GMAIL") {
			http.Redirect(w, r, "/dashboard", http.StatusFound)
			return
		}
	}

	templ.Handler(templates.Login()).ServeHTTP(w, r)
}

func HandleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("admin_token")
	if err == nil && cookie.Value != "" {
		email, err := services.ValidateJWT(cookie.Value)
		if err == nil && email == os.Getenv("ADMIN_GMAIL") {
			http.Redirect(w, r, "/dashboard", http.StatusFound)
			return
		}
	}

	url := services.GetGoogleAuthURL()
	http.Redirect(w, r, url, http.StatusFound)
}

func HandleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	ctx := r.Context()

	email, err := services.GetUserEmailFromCode(ctx, code)
	if err != nil {
		templ.Handler(templates.ErrorLoginIn("Failed to authenticate with Google. Please try again.")).ServeHTTP(w, r)
		return
	}

	if email != os.Getenv("ADMIN_GMAIL") {
		templ.Handler(templates.ErrorLoginIn(fmt.Sprintf("Access denied for %s. Developer access only.", email))).ServeHTTP(w, r)
		return
	}

	tokenString, err := services.CreateJWT(email)
	if err != nil {
		templ.Handler(templates.ErrorLoginIn("Internal Server Error")).ServeHTTP(w, r)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "admin_token",
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   true, 
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/dashboard", http.StatusFound)
}