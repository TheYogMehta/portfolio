package services

import (
	"context"
	"encoding/json"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func getOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}
}

func GetGoogleAuthURL() string {
	conf := getOAuthConfig()
	return conf.AuthCodeURL("state")
}

func GetUserEmailFromCode(ctx context.Context, code string) (string, error) {
	conf := getOAuthConfig()
	tok, err := conf.Exchange(ctx, code)
	if err != nil {
		return "", err
	}

	client := conf.Client(ctx, tok)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")

	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	var userInfo struct {
		Email string `json:"email"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return "", err
	}

	return userInfo.Email, nil
}