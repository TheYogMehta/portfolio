package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type PageStat struct {
	Path       string `json:"path"`
	Views      int    `json:"views"`
	Percentage int    `json:"percentage"`
}

type TrafficSource struct {
	Source string `json:"source"`
	Users  int    `json:"users"`
}

type DeviceStat struct {
	Device string `json:"device"`
	Users  int    `json:"users"`
}

type DailyStat struct {
	Date  string `json:"date"`
	Views int    `json:"views"`
}

type AnalyticsData struct {
	ActiveUsers int             `json:"activeUsers"`
	PageViews   int             `json:"pageViews"`
	AvgDuration string          `json:"avgDuration"`
	TopPages    []PageStat      `json:"topPages"`
	Sources     []TrafficSource `json:"sources"`
	Devices     []DeviceStat    `json:"devices"`
	DailyViews  []DailyStat     `json:"dailyViews"`
	DailyLabels string          `json:"dailyLabels"`
	DailyValues string          `json:"dailyValues"`
	LastError   string          `json:"lastError,omitempty"`
}

var (
	gaCacheData *AnalyticsData
	gaCacheTime time.Time
	gaCacheMu   sync.Mutex
)

func FetchGA4Metrics(ctx context.Context) *AnalyticsData {
	gaCacheMu.Lock()
	if gaCacheData != nil && time.Since(gaCacheTime) < 5*time.Minute {
		defer gaCacheMu.Unlock()
		return gaCacheData
	}
	gaCacheMu.Unlock()

	propertyID := strings.TrimSpace(os.Getenv("GA_PROPERTY_ID"))
	if propertyID == "" {
		return &AnalyticsData{
			LastError: "GA_PROPERTY_ID is missing in .env file",
		}
	}

	propertyID = strings.TrimPrefix(propertyID, "properties/")

	jsonKey := []byte(os.Getenv("GOOGLE_CREDENTIALS_JSON"))
	if len(jsonKey) == 0 {
		fileKey, err := os.ReadFile("credentials.json")
		if err == nil {
			jsonKey = fileKey
		}
	}

	if len(jsonKey) == 0 {
		return &AnalyticsData{
			LastError: "credentials.json file is missing in project root directory",
		}
	}

	creds, err := google.CredentialsFromJSON(ctx, jsonKey, "https://www.googleapis.com/auth/analytics.readonly")
	if err != nil {
		return &AnalyticsData{
			LastError: fmt.Sprintf("Failed to authenticate Google Service Account: %v", err),
		}
	}

	client := oauth2.NewClient(ctx, creds.TokenSource)
	url := fmt.Sprintf("https://analyticsdata.googleapis.com/v1beta/properties/%s:runReport", propertyID)

	data := &AnalyticsData{}

	reqTopPages := map[string]interface{}{
		"dateRanges": []map[string]string{
			{"startDate": "30daysAgo", "endDate": "today"},
		},
		"metrics": []map[string]string{
			{"name": "activeUsers"},
			{"name": "screenPageViews"},
			{"name": "userEngagementDuration"},
		},
		"dimensions": []map[string]string{
			{"name": "pagePath"},
		},
		"limit": 6,
	}

	jsonBytes1, _ := json.Marshal(reqTopPages)
	resp1, err := client.Post(url, "application/json", bytes.NewBuffer(jsonBytes1))
	if err != nil {
		return &AnalyticsData{LastError: fmt.Sprintf("Google Analytics API request failed: %v", err)}
	}
	defer resp1.Body.Close()

	body1, _ := io.ReadAll(resp1.Body)
	if resp1.StatusCode != http.StatusOK {
		return &AnalyticsData{LastError: fmt.Sprintf("GA4 API Error (Status %d): %s", resp1.StatusCode, string(body1))}
	}

	var gaResp1 struct {
		Rows []struct {
			DimensionValues []struct {
				Value string `json:"value"`
			} `json:"dimensionValues"`
			MetricValues []struct {
				Value string `json:"value"`
			} `json:"metricValues"`
		} `json:"rows"`
	}
	json.Unmarshal(body1, &gaResp1)

	totalUsers := 0
	totalViews := 0
	totalDurationSec := 0.0

	for _, row := range gaResp1.Rows {
		var users, views int
		var duration float64
		fmt.Sscanf(row.MetricValues[0].Value, "%d", &users)
		fmt.Sscanf(row.MetricValues[1].Value, "%d", &views)
		fmt.Sscanf(row.MetricValues[2].Value, "%f", &duration)

		totalUsers += users
		totalViews += views
		totalDurationSec += duration

		path := "/"
		if len(row.DimensionValues) > 0 {
			path = row.DimensionValues[0].Value
		}
		data.TopPages = append(data.TopPages, PageStat{Path: path, Views: views})
	}

	data.ActiveUsers = totalUsers
	data.PageViews = totalViews

	for i := range data.TopPages {
		if totalViews > 0 {
			data.TopPages[i].Percentage = (data.TopPages[i].Views * 100) / totalViews
		}
	}

	if totalUsers > 0 {
		avgSec := int(totalDurationSec / float64(totalUsers))
		if avgSec >= 60 {
			data.AvgDuration = fmt.Sprintf("%dm %ds", avgSec/60, avgSec%60)
		} else {
			data.AvgDuration = fmt.Sprintf("%ds", avgSec)
		}
	} else {
		data.AvgDuration = "0s"
	}

	reqDaily := map[string]interface{}{
		"dateRanges": []map[string]string{
			{"startDate": "7daysAgo", "endDate": "today"},
		},
		"metrics": []map[string]string{
			{"name": "screenPageViews"},
		},
		"dimensions": []map[string]string{
			{"name": "date"},
		},
	}

	jsonBytes2, _ := json.Marshal(reqDaily)
	resp2, err := client.Post(url, "application/json", bytes.NewBuffer(jsonBytes2))
	if err == nil && resp2.StatusCode == http.StatusOK {
		defer resp2.Body.Close()
		body2, _ := io.ReadAll(resp2.Body)

		var gaResp2 struct {
			Rows []struct {
				DimensionValues []struct {
					Value string `json:"value"`
				} `json:"dimensionValues"`
				MetricValues []struct {
					Value string `json:"value"`
				} `json:"metricValues"`
			} `json:"rows"`
		}
		json.Unmarshal(body2, &gaResp2)

		type tempDaily struct {
			rawDate string
			label   string
			views   int
		}
		var temp []tempDaily

		for _, row := range gaResp2.Rows {
			if len(row.DimensionValues) > 0 && len(row.MetricValues) > 0 {
				raw := row.DimensionValues[0].Value
				var views int
				fmt.Sscanf(row.MetricValues[0].Value, "%d", &views)

				label := raw
				if len(raw) == 8 {
					t, err := time.Parse("20060102", raw)
					if err == nil {
						label = t.Format("Jan 02")
					}
				}
				temp = append(temp, tempDaily{rawDate: raw, label: label, views: views})
			}
		}

		sort.Slice(temp, func(i, j int) bool {
			return temp[i].rawDate < temp[j].rawDate
		})

		var labels []string
		var values []string
		for _, d := range temp {
			data.DailyViews = append(data.DailyViews, DailyStat{Date: d.label, Views: d.views})
			labels = append(labels, fmt.Sprintf(`"%s"`, d.label))
			values = append(values, fmt.Sprintf("%d", d.views))
		}

		data.DailyLabels = "[" + strings.Join(labels, ",") + "]"
		data.DailyValues = "[" + strings.Join(values, ",") + "]"
	}

	if data.DailyLabels == "" {
		data.DailyLabels = `["Mon","Tue","Wed","Thu","Fri","Sat","Sun"]`
		data.DailyValues = `[0,0,0,0,0,0,0]`
	}

	reqSources := map[string]interface{}{
		"dateRanges": []map[string]string{
			{"startDate": "30daysAgo", "endDate": "today"},
		},
		"metrics": []map[string]string{
			{"name": "activeUsers"},
		},
		"dimensions": []map[string]string{
			{"name": "sessionSource"},
		},
		"limit": 4,
	}
	jsonBytes3, _ := json.Marshal(reqSources)
	resp3, err := client.Post(url, "application/json", bytes.NewBuffer(jsonBytes3))
	if err == nil && resp3.StatusCode == http.StatusOK {
		defer resp3.Body.Close()
		body3, _ := io.ReadAll(resp3.Body)
		var gaResp3 struct {
			Rows []struct {
				DimensionValues []struct {
					Value string `json:"value"`
				} `json:"dimensionValues"`
				MetricValues []struct {
					Value string `json:"value"`
				} `json:"metricValues"`
			} `json:"rows"`
		}
		json.Unmarshal(body3, &gaResp3)
		for _, row := range gaResp3.Rows {
			if len(row.DimensionValues) > 0 && len(row.MetricValues) > 0 {
				src := row.DimensionValues[0].Value
				if src == "(direct)" {
					src = "Direct"
				}
				var u int
				fmt.Sscanf(row.MetricValues[0].Value, "%d", &u)
				data.Sources = append(data.Sources, TrafficSource{Source: src, Users: u})
			}
		}
	}

	gaCacheMu.Lock()
	gaCacheData = data
	gaCacheTime = time.Now()
	gaCacheMu.Unlock()

	return data
}
