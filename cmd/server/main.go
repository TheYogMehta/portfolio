package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"time"

	"portfolio/cmd/templates"
	"portfolio/internal/db"
	"portfolio/internal/handlers"
	"portfolio/internal/middleware"
	"portfolio/internal/services"

	"github.com/a-h/templ"
	"github.com/joho/godotenv"
)

var DB *sql.DB

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		fmt.Println("[INFO] No .env file found or error loading .env, using system env")
	}

	// Connecting to PostgreSQL
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres:postgres@localhost:5432/portfolio?sslmode=disable"
	}
	var err error
	DB, err = db.InitDB(connStr)
	if err != nil {
		fmt.Printf("Fatal: PostgreSQL connection failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Connected to PostgreSQL successfully!")

	// Static file server
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// max 5 attempts per min
	authRateLimiter := middleware.NewRateLimiter(5, 1*time.Minute)

	// Public routes
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		stats := services.GetGitHubStats(ctx, DB)
		templ.Handler(templates.Home(stats)).ServeHTTP(w, r)
	})

	// Rate-limited Auth routes
	http.Handle("/login", authRateLimiter.Limit(http.HandlerFunc(handlers.HandleLogin)))
	http.Handle("/auth/google/login", authRateLimiter.Limit(http.HandlerFunc(handlers.HandleGoogleLogin)))
	http.Handle("/auth/google/callback", authRateLimiter.Limit(http.HandlerFunc(handlers.HandleGoogleCallback)))

	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     "admin_token",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
		})
		http.Redirect(w, r, "/", http.StatusFound)
	})

	// Protected Admin route (wrapped by middleware)
	http.Handle("/dashboard", middleware.RequireAdminAuth(templ.Handler(templates.Dashboard())))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	fmt.Printf("Listening on :%s\n", port)
	http.ListenAndServe(":"+port, nil)
}