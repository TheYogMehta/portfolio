package services

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"portfolio/internal/db"

	"github.com/golang-jwt/jwt/v4"
)

type TopPage struct {
	Path       string `json:"path"`
	Views      int    `json:"views"`
	Percentage int    `json:"percentage"`
}

type Source struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Users  int    `json:"users"`
}

type AnalyticsData struct {
	PageViews    int        `json:"page_views"`
	ActiveUsers  int        `json:"active_users"`
	AvgDuration  string     `json:"avg_duration"`
	DailyLabels  []string   `json:"daily_labels"`
	DailyViews   []int      `json:"daily_views"`
	DailyValues  []int      `json:"daily_values"`
	TopPages     []TopPage  `json:"top_pages"`
	Sources      []Source   `json:"sources"`
	LastError    string     `json:"last_error"`
	LastSyncedAt string     `json:"last_synced_at"`
}

type serviceAccountFile struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

func FetchGA4Metrics(ctx context.Context) *AnalyticsData {
	return &AnalyticsData{
		PageViews:    0,
		ActiveUsers:  0,
		AvgDuration:  "0s",
		DailyLabels:  []string{},
		DailyViews:   []int{},
		DailyValues:  []int{},
		TopPages:     []TopPage{},
		Sources:      []Source{},
		LastError:    "",
		LastSyncedAt: time.Now().Format("Jan 02, 15:04"),
	}
}

func StartGA4SyncWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = SyncGA4ProjectViews(ctx)
			}
		}
	}()
}

type ga4ReportResponse struct {
	Rows []struct {
		DimensionValues []struct {
			Value string `json:"value"`
		} `json:"dimensionValues"`
		MetricValues []struct {
			Value string `json:"value"`
		} `json:"metricValues"`
	} `json:"rows"`
}

func getGA4AccessToken(credentialsPath string) (string, error) {
	data, err := os.ReadFile(credentialsPath)
	if err != nil {
		return "", err
	}

	var sa serviceAccountFile
	if err := json.Unmarshal(data, &sa); err != nil {
		return "", err
	}

	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("failed to parse PEM block containing private key")
	}

	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}

	rsaKey, ok := parsedKey.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("private key is not RSA")
	}

	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   sa.ClientEmail,
		"scope": "https://www.googleapis.com/auth/analytics.readonly",
		"aud":   sa.TokenURI,
		"exp":   now.Add(1 * time.Hour).Unix(),
		"iat":   now.Unix(),
	})

	signedJWT, err := token.SignedString(rsaKey)
	if err != nil {
		return "", err
	}

	resp, err := http.PostForm(sa.TokenURI, map[string][]string{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {signedJWT},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GA4 token exchange failed: %s", string(bodyBytes))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return "", err
	}

	return tokenResp.AccessToken, nil
}

func SyncGA4ProjectViews(ctx context.Context) error {
	propertyID := os.Getenv("GA_PROPERTY_ID")
	if propertyID == "" {
		propertyID = "551451947"
	}

	if db.DB == nil {
		return fmt.Errorf("database unavailable")
	}

	credentialsPath := os.Getenv("GA_CREDENTIALS_FILE")
	if credentialsPath == "" {
		credentialsPath = "credentials.json"
	}

	accessToken, err := getGA4AccessToken(credentialsPath)
	if err != nil {
		log.Printf("[GA4 Sync Worker] Could not authenticate via %s: %v", credentialsPath, err)
		return err
	}

	url := fmt.Sprintf("https://analyticsdata.googleapis.com/v1beta/properties/%s:runReport", propertyID)
	requestBody := `{
		"dateRanges": [{"startDate": "2024-01-01", "endDate": "today"}],
		"dimensions": [{"name": "pagePath"}],
		"metrics": [{"name": "screenPageViews"}],
		"dimensionFilter": {
			"filter": {
				"fieldName": "pagePath",
				"stringFilter": {
					"matchType": "BEGINS_WITH",
					"value": "/project/view/"
				}
			}
		}
	}`

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(requestBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[GA4 Sync Worker Error] API returned status %d: %s", resp.StatusCode, string(bodyBytes))
		return nil
	}

	var report ga4ReportResponse
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return err
	}

	for _, row := range report.Rows {
		if len(row.DimensionValues) > 0 && len(row.MetricValues) > 0 {
			path := row.DimensionValues[0].Value
			slug := strings.TrimPrefix(path, "/project/view/")
			if idx := strings.Index(slug, "?"); idx != -1 {
				slug = slug[:idx]
			}
			slug = strings.TrimSpace(slug)
			if slug != "" {
				var count int
				_, _ = fmt.Sscanf(row.MetricValues[0].Value, "%d", &count)
				if count > 0 {
					_, _ = db.DB.ExecContext(ctx, "UPDATE projects SET views_count = $1 WHERE slug = $2", count, slug)
				}
			}
		}
	}
	return nil
}
