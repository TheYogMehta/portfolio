package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"portfolio/internal/services"
)

var slugRegex = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func parseAndValidateProjectForm(r *http.Request, isEdit bool, existingProject *services.ProjectRecord) (*services.ProjectRecord, error) {
	ctx := r.Context()

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		return nil, errors.New("[ERROR] Failed to parse uploaded form data")
	}

	// Title
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		return nil, errors.New("[ERROR] Project Title is required")
	}
	if len(title) < 2 || len(title) > 100 {
		return nil, errors.New("[ERROR] Project Title must be between 2 and 100 characters")
	}

	// Slug
	slug := strings.TrimSpace(r.FormValue("slug"))
	if slug == "" {
		return nil, errors.New("[ERROR] Project Slug is required")
	}
	if !slugRegex.MatchString(slug) {
		return nil, errors.New("[ERROR] Project Slug must contain only lowercase letters, numbers, and single hyphens (e.g. my-project-name)")
	}

	checkSlugUniqueness := true
	if isEdit && existingProject != nil && existingProject.Slug == slug {
		checkSlugUniqueness = false
	}
	if checkSlugUniqueness {
		slugExists, err := services.IsSlugExists(ctx, slug)
		if err != nil {
			return nil, errors.New("[ERROR] Database validation failed")
		}
		if slugExists {
			return nil, errors.New("[ERROR] A project with this slug already exists. Please choose a unique slug.")
		}
	}
	
	// featured
	isFeatured := r.FormValue("is_featured") == "true"
	if isFeatured {
		var excludeSlug string
		if isEdit && existingProject != nil {
			excludeSlug = existingProject.Slug
		}
		featuredCount, err := services.GetFeaturedCount(ctx, excludeSlug)
		if err != nil {
			return nil, errors.New("[ERROR] Database validation failed")
		}
		if featuredCount >= 2 {
			return nil, errors.New("[ERROR] Maximum of 2 featured projects allowed. Unfeature another project first.")
		}
	}

	// Thumbnail Image
	var thumbnailURL string
	if isEdit && existingProject != nil {
		thumbnailURL = existingProject.ThumbnailURL
	}

	file, header, err := r.FormFile("thumbnail")
	if err == nil {
		defer file.Close()
		if header.Size > 5<<20 {
			return nil, errors.New("[ERROR] Thumbnail image size must not exceed 5MB")
		}
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" && ext != ".svg" {
			return nil, errors.New("[ERROR] Thumbnail image must be a PNG, JPG, WebP, or SVG file")
		}
		savedURL, err := services.SaveThumbnailFile(file, header, slug)
		if err != nil {
			return nil, err
		}
		thumbnailURL = savedURL
	} else if !isEdit {
		return nil, errors.New("[ERROR] Project Thumbnail Image is required")
	}

	// GitHub Repository URL
	githubURL := strings.TrimSpace(r.FormValue("github_url"))
	if githubURL == "" {
		return nil, errors.New("[ERROR] GitHub Repository URL is required")
	}
	if !strings.HasPrefix(githubURL, "http://") && !strings.HasPrefix(githubURL, "https://") {
		return nil, errors.New("[ERROR] GitHub URL must begin with http:// or https://")
	}
	cleanURL := strings.TrimSpace(strings.TrimSuffix(githubURL, "/"))
	parts := strings.Split(cleanURL, "github.com/")
	if len(parts) < 2 {
		return nil, errors.New("[ERROR] Invalid GitHub Repository URL format")
	}
	pathParts := strings.Split(parts[1], "/")
	if len(pathParts) < 2 || pathParts[0] == "" || pathParts[1] == "" {
		return nil, errors.New("[ERROR] GitHub URL must include both owner and repository name (e.g. github.com/owner/repo)")
	}

	owner := pathParts[0]
	repoName := pathParts[1]
	gitData, err := services.FetchRepoData(owner, repoName)
	if err != nil {
		return nil, err
	}

	// Live Demo URL
	liveURL := strings.TrimSpace(r.FormValue("live_url"))
	if liveURL != "" {
		if !strings.HasPrefix(liveURL, "http://") && !strings.HasPrefix(liveURL, "https://") {
			return nil, errors.New("[ERROR] Live Demo URL must begin with http:// or https://")
		}
		if _, err := url.ParseRequestURI(liveURL); err != nil {
			return nil, errors.New("[ERROR] Invalid Live Demo URL format")
		}
	}

	// Tech Stack Tags
	tags := strings.TrimSpace(r.FormValue("tags"))
	if tags == "" {
		return nil, errors.New("[ERROR] Tech Stack Tags are required (e.g. Go, Templ, PostgreSQL)")
	}

	// Short Description
	shortDescription := strings.TrimSpace(r.FormValue("short_description"))
	if shortDescription == "" {
		return nil, errors.New("[ERROR] Project Short Description is required")
	}
	if len(shortDescription) < 10 {
		return nil, errors.New("[ERROR] Project Short Description must be at least 10 characters long")
	}

	// Long Description
	longDescription := strings.TrimSpace(r.FormValue("long_description"))

	return &services.ProjectRecord{
		Title:            title,
		Slug:             slug,
		GitHubURL:        githubURL,
		LiveURL:          liveURL,
		Tags:             tags,
		ShortDescription: shortDescription,
		LongDescription:  longDescription,
		IsPublic:         r.FormValue("is_public") == "true",
		IsGitHubPrivate:  gitData.IsPrivate,
		IsFeatured:       isFeatured,
		ThumbnailURL:     thumbnailURL,
		StarsCount:       gitData.StargazerCount,
		CommitsCount:     gitData.DefaultBranch.Target.History.TotalCount,
		Languages:        gitData.GetLanguageStats(),
	}, nil
}

func HandleCreateProject(w http.ResponseWriter, r *http.Request) {
	projectRecord, err := parseAndValidateProjectForm(r, false, nil)
	if err != nil {
		respondWithError(w, err.Error())
		return
	}

	if err := services.CreateProject(r.Context(), projectRecord); err != nil {
		respondWithError(w, err.Error())
		return
	}

	http.Redirect(w, r, "/dashboard/projects", http.StatusSeeOther)
}

func HandleEditProject(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/dashboard/projects/edit/")
	slug = strings.TrimSpace(slug)
	if slug == "" {
		respondWithError(w, "[ERROR] Missing project slug")
		return
	}

	existingProject, err := services.GetProjectBySlug(r.Context(), slug)
	if err != nil {
		respondWithError(w, "[ERROR] Project not found")
		return
	}

	projectRecord, err := parseAndValidateProjectForm(r, true, existingProject)
	if err != nil {
		respondWithError(w, err.Error())
		return
	}

	if err := services.UpdateProject(r.Context(), slug, projectRecord); err != nil {
		respondWithError(w, err.Error())
		return
	}

	http.Redirect(w, r, "/dashboard/projects", http.StatusSeeOther)
}

func HandleDeleteProject(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/dashboard/projects/delete/")
	slug = strings.TrimSpace(slug)
	if slug == "" {
		respondWithError(w, "[ERROR] Missing project slug")
		return
	}

	if err := services.DeleteProject(r.Context(), slug); err != nil {
		respondWithError(w, err.Error())
		return
	}

	http.Redirect(w, r, "/dashboard/projects", http.StatusSeeOther)
}

func respondWithError(w http.ResponseWriter, errMsg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": errMsg})
}