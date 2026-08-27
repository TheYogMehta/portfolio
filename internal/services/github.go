package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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

	fi, errStat := os.Stat("static/images/github_chart.svg")
	if os.IsNotExist(errStat) || fi.Size() == 0 {
		downloadLatestChartSVG()
	}

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
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://github-contributions-api.jogruber.de/v4/theYogMehta?y=last")
	if err != nil || resp.StatusCode != http.StatusOK {
		return "", false
	}
	defer resp.Body.Close()

	var data ghResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.Total.LastYear == 0 {
		return "", false
	}

	return fmt.Sprintf("%d commits", data.Total.LastYear), true
}

func downloadLatestChartSVG() {
	client := http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get("https://ghchart.rshah.org/39d353/theYogMehta")
	if err == nil && resp.StatusCode == http.StatusOK {
		_ = os.MkdirAll("static/images", 0755)
		file, err := os.Create("static/images/github_chart.svg")
		if err == nil {
			io.Copy(file, resp.Body)
			file.Close()
		}
		resp.Body.Close()
	}
}

type LanguageStat struct {
	Name       string  `json:"name"`
	Color      string  `json:"color"`
	Size       int     `json:"size"`
	Percentage float64 `json:"percentage"`
}

type RepoData struct {
	Name           string `json:"name"`
	IsPrivate      bool   `json:"isPrivate"`
	StargazerCount int    `json:"stargazerCount"`
	ForkCount      int    `json:"forkCount"`
	DefaultBranch  struct {
		Target struct {
			History struct {
				TotalCount int `json:"totalCount"`
			} `json:"history"`
		} `json:"target"`
	} `json:"defaultBranchRef"`
	Languages struct {
		TotalSize int `json:"totalSize"`
		Edges     []struct {
			Size int `json:"size"`
			Node struct {
				Name  string `json:"name"`
				Color string `json:"color"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"languages"`
}

func (r *RepoData) GetLanguageStats() []LanguageStat {
	var stats []LanguageStat
	if r.Languages.TotalSize == 0 {
		return stats
	}

	for _, edge := range r.Languages.Edges {
		pct := (float64(edge.Size) / float64(r.Languages.TotalSize)) * 100
		stats = append(stats, LanguageStat{
			Name:       edge.Node.Name,
			Color:      edge.Node.Color,
			Size:       edge.Size,
			Percentage: pct,
		})
	}
	return stats
}

type DynamicGraphQLResponse struct {
	Data map[string]RepoData `json:"data"`
}

func FetchRepoData(owner, repoName string) (*RepoData, error) {
	dataMap, err := FetchSelectedRepos(owner, []string{repoName})
	if err != nil {
		return nil, err
	}
	repoData, exists := dataMap["repo_0"]
	if !exists {
		return nil, errors.New("[ERROR] Repository data not found")
	}
	return &repoData, nil
}

func FetchSelectedRepos(owner string, repoNames []string) (map[string]RepoData, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, errors.New("[ERROR] GitHub token missing")
	}

	var repoQueries strings.Builder
	for i, name := range repoNames {
		alias := fmt.Sprintf("repo_%d", i)
		repoQueries.WriteString(fmt.Sprintf(`%s: repository(owner: "%s", name: "%s") { ...RepoDetails } `, alias, owner, name))
	}

	query := fmt.Sprintf(`
		fragment RepoDetails on Repository {
			name
			isPrivate
			stargazerCount
			forkCount
			defaultBranchRef {
				target {
					... on Commit {
						history {
							totalCount
						}
					}
				}
			}
			languages(first: 10, orderBy: {field: SIZE, direction: DESC}) {
				totalSize
				edges {
					size
					node {
						name
						color
					}
				}
			}
		}
		query { %s }
	`, repoQueries.String())

	payload, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, errors.New("[ERROR] Failed to process request payload")
	}

	req, err := http.NewRequest("POST", "https://api.github.com/graphql", bytes.NewBuffer(payload))
	if err != nil {
		return nil, errors.New("[ERROR] Failed to build GitHub API request")
	}

	req.Header.Set("Authorization", "bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("[ERROR] GitHub API connection failed")
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("[ERROR] GitHub API returned status %d", res.StatusCode)
	}

	var result DynamicGraphQLResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, errors.New("[ERROR] Failed to parse GitHub API response")
	}

	return result.Data, nil
}