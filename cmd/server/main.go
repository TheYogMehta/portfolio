package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"

	"portfolio/cmd/templates"
	"portfolio/internal/db"
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

	// Home route handler
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		stats := services.GetGitHubStats(ctx, DB)
		templ.Handler(templates.Home(stats)).ServeHTTP(w, r)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	fmt.Printf("Listening on :%s\n", port)
	http.ListenAndServe(":"+port, nil)
}