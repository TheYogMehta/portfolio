package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"portfolio/internal/db"
)

type ProjectRecord struct {
	ID              int
	Title           string
	Slug            string
	GitHubURL       string
	LiveURL         string
	Tags            string
	ShortDescription string
	LongDescription  string
	IsPublic        bool
	IsGitHubPrivate bool
	IsFeatured      bool
	ThumbnailURL    string
	StarsCount      int
	CommitsCount    int
	ViewsCount      int
	Languages       []LanguageStat
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func IsSlugExists(ctx context.Context, slug string) (bool, error) {
	if db.DB == nil {
		return false, errors.New("[ERROR] Database connection unavailable")
	}

	var count int
	err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE slug = $1`, slug).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func GetFeaturedCount(ctx context.Context, excludeSlug string) (int, error) {
	if db.DB == nil {
		return 0, errors.New("[ERROR] Database connection unavailable")
	}

	var count int
	var err error

	if excludeSlug != "" {
		err = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE is_featured = true AND slug <> $1`, excludeSlug).Scan(&count)
	} else {
		err = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE is_featured = true`).Scan(&count)
	}

	if err != nil {
		return 0, err
	}
	return count, nil
}

func SaveThumbnailFile(file multipart.File, header *multipart.FileHeader, slug string) (string, error) {
	if err := os.MkdirAll("static/uploads", 0755); err != nil {
		return "", errors.New("[ERROR] Failed to create uploads directory")
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	uniqueFilename := fmt.Sprintf("%s-%d%s", slug, time.Now().Unix(), ext)
	dstPath := filepath.Join("static/uploads", uniqueFilename)

	dst, err := os.Create(dstPath)
	if err != nil {
		return "", errors.New("[ERROR] Failed to save thumbnail image file")
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return "", errors.New("[ERROR] Failed to copy thumbnail file contents")
	}

	return "/static/uploads/" + uniqueFilename, nil
}

func CreateProject(ctx context.Context, p *ProjectRecord) error {
	if db.DB == nil {
		return errors.New("[ERROR] Database connection unavailable")
	}

	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("[ERROR] Failed to initiate database transaction")
	}
	defer tx.Rollback()

	var projectID int
	query := `
		INSERT INTO projects (
			title, slug, github_url, live_url, tags, short_description, long_description,
			is_public, is_github_private, is_featured, thumbnail_url,
			stars_count, commits_count
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		) RETURNING id
	`

	err = tx.QueryRowContext(
		ctx, query,
		p.Title, p.Slug, p.GitHubURL, p.LiveURL, p.Tags, p.ShortDescription, p.LongDescription,
		p.IsPublic, p.IsGitHubPrivate, p.IsFeatured, p.ThumbnailURL,
		p.StarsCount, p.CommitsCount,
	).Scan(&projectID)

	if err != nil {
		return fmt.Errorf("[ERROR] Failed to insert project record into database: %v", err)
	}

	for _, lang := range p.Languages {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO project_languages (project_id, name, percentage)
			VALUES ($1, $2, $3)
			ON CONFLICT (project_id, name) DO UPDATE SET percentage = $3
		`, projectID, lang.Name, lang.Percentage)
		if err != nil {
			return fmt.Errorf("[ERROR] Failed to insert project language data: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return errors.New("[ERROR] Failed to commit project database transaction")
	}

	return nil
}

func GetProject(ctx context.Context, page int) ([]ProjectRecord, int, error) {
	if db.DB == nil {
		return nil, 0, fmt.Errorf("[ERROR] Database connection unavailable")
	}

	if page <= 0 {
		page = 1
	}

	offset := (page - 1) * 10

	query := `
		SELECT 
			p.slug,
			p.title, 
			p.github_url, 
			COALESCE(p.live_url, ''), 
			COALESCE(p.tags, ''), 
			COALESCE(p.short_description, ''), 
			COALESCE(p.long_description, ''), 
			p.is_public, 
			p.is_github_private, 
			p.is_featured, 
			COALESCE(p.thumbnail_url, ''), 
			p.stars_count, 
			p.commits_count, 
			p.views_count,
			COUNT(*) OVER() AS full_count,
			COALESCE(
				json_agg(
					json_build_object('name', pl.name, 'percentage', pl.percentage)
				) FILTER (WHERE pl.name IS NOT NULL),
				'[]'
			) AS languages_json
		FROM projects p
		LEFT JOIN project_languages pl ON p.id = pl.project_id
		GROUP BY p.id
		ORDER BY p.created_at DESC
		LIMIT 10 OFFSET $1
	`

	rows, err := db.DB.QueryContext(ctx, query, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch projects: %w", err)
	}
	defer rows.Close()

	var projects []ProjectRecord
	var fullCount int

	for rows.Next() {
		var p ProjectRecord
		var languagesJSON []byte

		if err := rows.Scan(
			&p.Slug, &p.Title, &p.GitHubURL, &p.LiveURL, &p.Tags, &p.ShortDescription, &p.LongDescription,
			&p.IsPublic, &p.IsGitHubPrivate, &p.IsFeatured, &p.ThumbnailURL,
			&p.StarsCount, &p.CommitsCount, &p.ViewsCount,
			&fullCount, &languagesJSON,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan project row: %w", err)
		}

		if len(languagesJSON) > 0 {
			_ = json.Unmarshal(languagesJSON, &p.Languages)
		}

		projects = append(projects, p)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("row iteration error: %w", err)
	}

	totalPages := 0
	if fullCount > 0 {
		totalPages = (fullCount + 9) / 10
	}

	return projects, totalPages, nil
}

func IncrementProjectViews(ctx context.Context, slug string) error {
	if db.DB == nil {
		return fmt.Errorf("[ERROR] Database connection unavailable")
	}
	_, err := db.DB.ExecContext(ctx, "UPDATE projects SET views_count = views_count + 1 WHERE slug = $1 AND is_public = true", slug)
	return err
}

func GetProjectBySlug(ctx context.Context, slug string) (*ProjectRecord, error) {
	if db.DB == nil {
		return nil, errors.New("[ERROR] Database connection unavailable")
	}

	query := `
		SELECT 
			p.id, 
			p.title, 
			p.slug, 
			p.github_url, 
			COALESCE(p.live_url, ''), 
			COALESCE(p.tags, ''), 
			COALESCE(p.short_description, ''), 
			COALESCE(p.long_description, ''), 
			p.is_public, 
			p.is_github_private, 
			p.is_featured, 
			COALESCE(p.thumbnail_url, ''), 
			p.stars_count, 
			p.commits_count, 
			p.views_count, 
			p.created_at, 
			p.updated_at,
			COALESCE(
				json_agg(
					json_build_object('name', pl.name, 'percentage', pl.percentage)
				) FILTER (WHERE pl.name IS NOT NULL),
				'[]'
			) AS languages_json
		FROM projects p
		LEFT JOIN project_languages pl ON p.id = pl.project_id
		WHERE p.slug = $1
		GROUP BY p.id
	`

	var p ProjectRecord
	var languagesJSON []byte

	err := db.DB.QueryRowContext(ctx, query, slug).Scan(
		&p.ID, &p.Title, &p.Slug, &p.GitHubURL, &p.LiveURL, &p.Tags, &p.ShortDescription, &p.LongDescription,
		&p.IsPublic, &p.IsGitHubPrivate, &p.IsFeatured, &p.ThumbnailURL,
		&p.StarsCount, &p.CommitsCount, &p.ViewsCount, &p.CreatedAt, &p.UpdatedAt,
		&languagesJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	if len(languagesJSON) > 0 {
		_ = json.Unmarshal(languagesJSON, &p.Languages)
	}

	return &p, nil
}

func DeleteThumbnailFile(thumbnailURL string) {
	if thumbnailURL == "" || !strings.HasPrefix(thumbnailURL, "/static/uploads/") {
		return
	}
	filePath := strings.TrimPrefix(thumbnailURL, "/")
	if _, err := os.Stat(filePath); err == nil {
		_ = os.Remove(filePath)
	}
}

func UpdateProject(ctx context.Context, originalSlug string, p *ProjectRecord) error {
	if db.DB == nil {
		return errors.New("[ERROR] Database connection unavailable")
	}

	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("[ERROR] Failed to initiate database transaction")
	}
	defer tx.Rollback()

	var projectID int
	var oldThumbnailURL string
	err = tx.QueryRowContext(ctx, `SELECT id, COALESCE(thumbnail_url, '') FROM projects WHERE slug = $1`, originalSlug).Scan(&projectID, &oldThumbnailURL)
	if err != nil {
		return fmt.Errorf("[ERROR] Existing project not found: %w", err)
	}

	query := `
		UPDATE projects SET
			title = $1,
			slug = $2,
			github_url = $3,
			live_url = $4,
			tags = $5,
			short_description = $6,
			long_description = $7,
			is_public = $8,
			is_github_private = $9,
			is_featured = $10,
			thumbnail_url = CASE WHEN $11 <> '' THEN $11 ELSE thumbnail_url END,
			stars_count = $12,
			commits_count = $13,
			updated_at = NOW()
		WHERE id = $14
	`

	_, err = tx.ExecContext(
		ctx, query,
		p.Title, p.Slug, p.GitHubURL, p.LiveURL, p.Tags, p.ShortDescription, p.LongDescription,
		p.IsPublic, p.IsGitHubPrivate, p.IsFeatured, p.ThumbnailURL,
		p.StarsCount, p.CommitsCount, projectID,
	)
	if err != nil {
		return fmt.Errorf("[ERROR] Failed to update project record: %w", err)
	}

	_, _ = tx.ExecContext(ctx, `DELETE FROM project_languages WHERE project_id = $1`, projectID)

	for _, lang := range p.Languages {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO project_languages (project_id, name, percentage)
			VALUES ($1, $2, $3)
			ON CONFLICT (project_id, name) DO UPDATE SET percentage = $3
		`, projectID, lang.Name, lang.Percentage)
		if err != nil {
			return fmt.Errorf("[ERROR] Failed to update project language data: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return errors.New("[ERROR] Failed to commit project database transaction")
	}

	if p.ThumbnailURL != "" && p.ThumbnailURL != oldThumbnailURL {
		DeleteThumbnailFile(oldThumbnailURL)
	}

	return nil
}

func DeleteProject(ctx context.Context, slug string) error {
	if db.DB == nil {
		return errors.New("[ERROR] Database connection unavailable")
	}

	var thumbnailURL string
	_ = db.DB.QueryRowContext(ctx, `SELECT COALESCE(thumbnail_url, '') FROM projects WHERE slug = $1`, slug).Scan(&thumbnailURL)

	_, err := db.DB.ExecContext(ctx, `DELETE FROM projects WHERE slug = $1`, slug)
	if err != nil {
		return fmt.Errorf("[ERROR] Failed to delete project: %w", err)
	}

	if thumbnailURL != "" {
		DeleteThumbnailFile(thumbnailURL)
	}

	return nil
}

func GetFeaturedProjects(ctx context.Context) ([]ProjectRecord, error) {
	if db.DB == nil {
		return nil, fmt.Errorf("[ERROR] Database connection unavailable")
	}

	query := `
		SELECT 
			p.slug,
			p.title, 
			p.github_url, 
			COALESCE(p.live_url, ''), 
			COALESCE(p.tags, ''), 
			COALESCE(p.short_description, ''), 
			COALESCE(p.long_description, ''), 
			p.is_public, 
			p.is_github_private, 
			p.is_featured, 
			COALESCE(p.thumbnail_url, ''), 
			p.stars_count, 
			p.commits_count, 
			p.views_count,
			COALESCE(
				json_agg(
					json_build_object('name', pl.name, 'percentage', pl.percentage)
				) FILTER (WHERE pl.name IS NOT NULL),
				'[]'
			) AS languages_json
		FROM projects p
		LEFT JOIN project_languages pl ON p.id = pl.project_id
		WHERE p.is_public = true AND p.is_featured = true
		GROUP BY p.id
		ORDER BY p.created_at DESC
		LIMIT 2
	`

	rows, err := db.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("[ERROR] Failed to query featured projects: %w", err)
	}
	defer rows.Close()

	var projects []ProjectRecord

	for rows.Next() {
		var p ProjectRecord
		var languagesJSON []byte

		if err := rows.Scan(
			&p.Slug, &p.Title, &p.GitHubURL, &p.LiveURL, &p.Tags, &p.ShortDescription, &p.LongDescription,
			&p.IsPublic, &p.IsGitHubPrivate, &p.IsFeatured, &p.ThumbnailURL,
			&p.StarsCount, &p.CommitsCount, &p.ViewsCount,
			&languagesJSON,
		); err != nil {
			return nil, fmt.Errorf("[ERROR] Failed to scan featured project row: %w", err)
		}

		if len(languagesJSON) > 0 {
			if err := json.Unmarshal(languagesJSON, &p.Languages); err != nil {
				p.Languages = []LanguageStat{}
			}
		}

		projects = append(projects, p)
	}

	return projects, nil
}

func GetPublicProjects(ctx context.Context, page int) ([]ProjectRecord, int, error) {
	if db.DB == nil {
		return nil, 0, fmt.Errorf("[ERROR] Database connection unavailable")
	}

	if page <= 0 {
		page = 1
	}

	offset := (page - 1) * 9

	query := `
		SELECT 
			p.slug,
			p.title, 
			p.github_url, 
			COALESCE(p.live_url, ''), 
			COALESCE(p.tags, ''), 
			COALESCE(p.short_description, ''), 
			COALESCE(p.long_description, ''), 
			p.is_public, 
			p.is_github_private, 
			p.is_featured, 
			COALESCE(p.thumbnail_url, ''), 
			p.stars_count, 
			p.commits_count, 
			p.views_count,
			COUNT(*) OVER() AS full_count,
			COALESCE(
				json_agg(
					json_build_object('name', pl.name, 'percentage', pl.percentage)
				) FILTER (WHERE pl.name IS NOT NULL),
				'[]'
			) AS languages_json
		FROM projects p
		LEFT JOIN project_languages pl ON p.id = pl.project_id
		WHERE p.is_public = true
		GROUP BY p.id
		ORDER BY p.is_featured DESC, p.created_at DESC
		LIMIT 9 OFFSET $1
	`

	rows, err := db.DB.QueryContext(ctx, query, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch public projects: %w", err)
	}
	defer rows.Close()

	var projects []ProjectRecord
	var fullCount int

	for rows.Next() {
		var p ProjectRecord
		var languagesJSON []byte

		if err := rows.Scan(
			&p.Slug, &p.Title, &p.GitHubURL, &p.LiveURL, &p.Tags, &p.ShortDescription, &p.LongDescription,
			&p.IsPublic, &p.IsGitHubPrivate, &p.IsFeatured, &p.ThumbnailURL,
			&p.StarsCount, &p.CommitsCount, &p.ViewsCount,
			&fullCount, &languagesJSON,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan public project row: %w", err)
		}

		if len(languagesJSON) > 0 {
			if err := json.Unmarshal(languagesJSON, &p.Languages); err != nil {
				p.Languages = []LanguageStat{}
			}
		}

		projects = append(projects, p)
	}

	totalPages := (fullCount + 8) / 9
	return projects, totalPages, nil
}