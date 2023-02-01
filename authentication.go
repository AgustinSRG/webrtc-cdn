// Authentication

package main

import (
	"fmt"
	"os"

	"github.com/golang-jwt/jwt"
)

func checkAuthentication(auth string, expectedSubject string, streamId string) bool {
	var JWT_SECRET = os.Getenv("JWT_SECRET")

	if JWT_SECRET == "" {
		return true // No authentication required
	}

	if auth == "" {
		return false // Authentication required, but not provided
	}

	token, err := jwt.Parse(auth, func(token *jwt.Token) (interface{}, error) {
		// Check the algorithm
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
		}

		// Provide signing key
		return []byte(JWT_SECRET), nil
	})

	if err != nil {
		return false // Invalid token
	}

	claims, ok := token.Claims.(jwt.MapClaims)

	if !ok || !token.Valid {
		return false // Invalid token
	}

	if claims["sub"] == nil || claims["sub"].(string) != expectedSubject {
		return false // Invalid subject
	}

	if claims["sid"] == nil || claims["sid"].(string) != streamId {
		return false // Not for this stream
	}

	return true // Valid
}
