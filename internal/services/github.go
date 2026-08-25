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

func GetGitHubStats(ctx context.Context, db *sql.DB) GitHubStats {
	var currentStats GitHubStats
	var updatedAt time.Time

	err := db.QueryRowContext(ctx, `SELECT commits, last_updated, updated_at FROM github_stats WHERE id = 1`).Scan(&currentStats.Commits, &currentStats.LastUpdated, &updatedAt)

	if err == nil && time.Since(updatedAt) < 1*time.Hour {
		return currentStats
	}

	freshCount, ok := fetchCommitCountFromAPI()
	if !ok {
		return currentStats
	}

	if currentStats.Commits != freshCount || err != nil {
		downloadLatestChartSVG()

		freshStats := GitHubStats{
			Commits:     freshCount,
			LastUpdated: time.Now().Format("Jan 02, 15:04"),
		}

		_, _ = db.ExecContext(ctx, `
		INSERT INTO github_stats (id, commits, last_updated, updated_at)
		VALUES (1, $1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (id) 
		DO UPDATE SET commits = $1, last_updated = $2, updated_at = CURRENT_TIMESTAMP;`, freshStats.Commits, freshStats.LastUpdated)
		return freshStats
	}

	return currentStats
}

func fetchCommitCountFromAPI() (string, bool) {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://github-contributions-api.jogruber.de/v4/theYogMehta?y=last")
	if err != nil || resp.StatusCode != http.StatusOK {
		return "", false
	}
	defer resp.Body.Close()

	var data ghResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.Total.LastYear == 0 {
		return "", false
	}

	return fmt.Sprintf("%d+ commits", data.Total.LastYear), true
}

func downloadLatestChartSVG() {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://ghchart.rshah.org/39d353/theYogMehta")
	if err == nil && resp.StatusCode == http.StatusOK {
		file, err := os.Create("static/images/github_chart.svg")
		if err == nil {
			io.Copy(file, resp.Body)
			file.Close()
		}
		resp.Body.Close()
	}
}