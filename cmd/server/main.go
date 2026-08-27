package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"portfolio/cmd/templates"
	"portfolio/internal/db"
	"portfolio/internal/handlers"
	"portfolio/internal/middleware"
	"portfolio/internal/services"

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
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			templates.NotFound().Render(r.Context(), w)
			return
		}

		ctx := r.Context()
		stats := services.GetGitHubStats(ctx, DB)
		featuredProjects, _ := services.GetFeaturedProjects(ctx)
		templates.Home(stats, featuredProjects).Render(ctx, w)
	})

	http.HandleFunc("/projects", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		pageStr := r.URL.Query().Get("page")
		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}

		projects, totalPages, err := services.GetPublicProjects(ctx, page)
		templates.PublicProjects(projects, page, totalPages, err).Render(ctx, w)
	})

	http.HandleFunc("/project/view/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		slug := strings.TrimPrefix(r.URL.Path, "/project/view/")
		if slug == "" {
			http.Redirect(w, r, "/projects", http.StatusSeeOther)
			return
		}

		_ = services.IncrementProjectViews(ctx, slug)
		project, err := services.GetProjectBySlug(ctx, slug)
		if err != nil || project == nil || !project.IsPublic {
			w.WriteHeader(http.StatusNotFound)
			templates.ProjectDetail(nil, fmt.Errorf("Project not found")).Render(ctx, w)
			return
		}

		templates.ProjectDetail(project, nil).Render(ctx, w)
	})
	
	http.HandleFunc("/contact", func(w http.ResponseWriter, r *http.Request) {
		templates.Contact().Render(r.Context(), w)
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

	// Protected Admin route
	http.Handle("/dashboard", middleware.RequireAdminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		gaData := services.FetchGA4Metrics(ctx)
		templates.Dashboard(gaData).Render(ctx, w)
	})))

	http.Handle("/dashboard/projects", middleware.RequireAdminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		
		// page from query ?page=1
		pageStr := r.URL.Query().Get("page")
		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}

		projects, totalPages, err := services.GetProject(ctx, page)
		templates.DashboardProjects(projects, page, totalPages, err).Render(ctx, w)
	})))

	http.Handle("/dashboard/projects/new", middleware.RequireAdminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if r.Method == http.MethodPost {
			handlers.HandleCreateProject(w, r)
			return
		}
		templates.DashboardProjectNewEdit(nil).Render(ctx, w)
	})))

	http.Handle("/dashboard/projects/edit/", middleware.RequireAdminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		slug := strings.TrimPrefix(r.URL.Path, "/dashboard/projects/edit/")
		if slug == "" {
			http.Redirect(w, r, "/dashboard/projects", http.StatusSeeOther)
			return
		}

		if r.Method == http.MethodPost {
			handlers.HandleEditProject(w, r)
			return
		}

		project, err := services.GetProjectBySlug(ctx, slug)
		if err != nil {
			http.Redirect(w, r, "/dashboard/projects?error="+url.QueryEscape("Project not found"), http.StatusSeeOther)
			return
		}

		templates.DashboardProjectNewEdit(project).Render(ctx, w)
	})))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	fmt.Printf("Listening on :%s\n", port)
	http.ListenAndServe(":"+port, nil)
}