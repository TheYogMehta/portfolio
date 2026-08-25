package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type GitHubStats struct {
	Commits     string `json:"commits"`
	LastUpdated string `json:"last_updated"`
}

type ghResponse struct {
	Total struct {
		LastYear int `json:"lastYear"`
	} `json:"total"`
}

// GetGitHubStats handles PostgreSQL caching with a 1-hour expiration
func GetGitHubStats(ctx context.Context, db *sql.DB) GitHubStats {
	if db == nil {
		return fetchGithubData()
	}

	var stats GitHubStats
	var updatedAt time.Time

	// 1. Try reading from PostgreSQL
	query := `SELECT commits, last_updated, updated_at FROM github_stats WHERE id = 1`
	err := db.QueryRowContext(ctx, query).Scan(&stats.Commits, &stats.LastUpdated, &updatedAt)

	// 2. Check if cache exists and is less than 1 hour old
	if err == nil && time.Since(updatedAt) < 1*time.Hour {
		return stats // ⚡ Cache Hit: Returns from PostgreSQL in ~1ms
	}

	// 3. Cache Miss / Expired: Fetch fresh data from GitHub API & download new SVG
	freshStats := fetchGithubData()

	// 4. Upsert (Insert or Update) into PostgreSQL
	upsertQuery := `
	INSERT INTO github_stats (id, commits, last_updated, updated_at)
	VALUES (1, $1, $2, CURRENT_TIMESTAMP)
	ON CONFLICT (id) 
	DO UPDATE SET commits = $1, last_updated = $2, updated_at = CURRENT_TIMESTAMP;`

	_, _ = db.ExecContext(ctx, upsertQuery, freshStats.Commits, freshStats.LastUpdated)

	return freshStats
}

func fetchGithubData() GitHubStats {
	client := http.Client{Timeout: 3 * time.Second}
	commitsText := "2,837+ commits"

	// Fetch Commit Count JSON
	resp, err := client.Get("https://github-contributions-api.jogruber.de/v4/theYogMehta?y=last")
	if err == nil && resp.StatusCode == http.StatusOK {
		var data ghResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil && data.Total.LastYear > 0 {
			commitsText = fmt.Sprintf("%d+ commits", data.Total.LastYear)
		}
		resp.Body.Close()
	}

	// Download & Save SVG Chart locally
	chartResp, err := client.Get("https://ghchart.rshah.org/39d353/theYogMehta")
	if err == nil && chartResp.StatusCode == http.StatusOK {
		file, err := os.Create("static/images/github_chart.svg")
		if err == nil {
			io.Copy(file, chartResp.Body)
			file.Close()
		}
		chartResp.Body.Close()
	}

	return GitHubStats{
		Commits:     commitsText,
		LastUpdated: time.Now().Format("Jan 02, 15:04"),
	}
}
