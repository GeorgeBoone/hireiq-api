package middleware

import (
	"context"
	"net/http"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"google.golang.org/api/option"
)

const (
	// ContextKeyFirebaseUID is the key for the Firebase UID in the Gin context
	ContextKeyFirebaseUID = "firebase_uid"
	// ContextKeyUserID is the key for the internal user UUID in the Gin context
	ContextKeyUserID = "user_id"
)

// AuthMiddleware validates Firebase ID tokens and injects the UID into context
type AuthMiddleware struct {
	client *auth.Client
}

// NewAuthMiddleware creates a new Firebase auth middleware
func NewAuthMiddleware(projectID string) (*AuthMiddleware, error) {
	ctx := context.Background()

	var app *firebase.App
	var err error

	if projectID != "" {
		conf := &firebase.Config{ProjectID: projectID}
		app, err = firebase.NewApp(ctx, conf)
	} else {
		// Falls back to GOOGLE_APPLICATION_CREDENTIALS or default credentials
		app, err = firebase.NewApp(ctx, nil, option.WithoutAuthentication())
	}

	if err != nil {
		return nil, err
	}

	client, err := app.Auth(ctx)
	if err != nil {
		return nil, err
	}

	return &AuthMiddleware{client: client}, nil
}

// Authenticate is the Gin middleware handler
func (am *AuthMiddleware) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Missing Authorization header",
			})
			return
		}

		// Expect "Bearer <token>"
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid Authorization header format",
			})
			return
		}

		token, err := am.client.VerifyIDToken(c.Request.Context(), parts[1])
		if err != nil {
			log.Warn().Err(err).Msg("Failed to verify Firebase token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid or expired token",
			})
			return
		}

		// Inject Firebase UID into context
		c.Set(ContextKeyFirebaseUID, token.UID)

		// Extract email if available
		if email, ok := token.Claims["email"].(string); ok {
			c.Set("email", email)
		}

		c.Next()
	}
}

// GetFirebaseUID extracts the Firebase UID from the Gin context
func GetFirebaseUID(c *gin.Context) string {
	uid, _ := c.Get(ContextKeyFirebaseUID)
	if s, ok := uid.(string); ok {
		return s
	}
	return ""
}

// GetUserID extracts the internal user UUID from the Gin context
func GetUserID(c *gin.Context) string {
	uid, _ := c.Get(ContextKeyUserID)
	if s, ok := uid.(string); ok {
		return s
	}
	return ""
}
