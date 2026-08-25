package services

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

func getJWTSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatalf("❌ FATAL: JWT_SECRET environment variable is missing in .env! Cannot start auth service.")
	}
	return []byte(secret)
}

func CreateJWT(email string) (string, error) {
	secretKey := getJWTSecret()

	claims := Claims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secretKey)
}

func ValidateJWT(tokenString string) (string, error) {
	secretKey := getJWTSecret()

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secretKey, nil
	})

	if err != nil || !token.Valid {
		return "", fmt.Errorf("invalid or expired token")
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return "", fmt.Errorf("invalid token claims")
	}
	return claims.Email, nil
}
