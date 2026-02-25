package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/middleware"
	"github.com/yourusername/hireiq-api/internal/model"
	"github.com/yourusername/hireiq-api/internal/repository"
)

type AuthHandler struct {
	userRepo *repository.UserRepo
}

func NewAuthHandler(userRepo *repository.UserRepo) *AuthHandler {
	return &AuthHandler{userRepo: userRepo}
}

// GoogleSignIn handles POST /auth/google
// Creates or fetches a user based on Firebase token
func (h *AuthHandler) GoogleSignIn(c *gin.Context) {
	firebaseUID := middleware.GetFirebaseUID(c)
	if firebaseUID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	email, _ := c.Get("email")
	emailStr, _ := email.(string)

	// Check if user exists
	user, err := h.userRepo.FindByFirebaseUID(c.Request.Context(), firebaseUID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to look up user")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}

	// Create if new
	if user == nil {
		var req struct {
			Name string `json:"name"`
		}
		c.ShouldBindJSON(&req)

		user, err = h.userRepo.Create(c.Request.Context(), firebaseUID, emailStr, req.Name)
		if err != nil {
			log.Error().Err(err).Msg("Failed to create user")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create account"})
			return
		}
		log.Info().Str("uid", firebaseUID).Msg("New user created")
	}

	c.JSON(http.StatusOK, user)
}

// ProfileHandler handles profile CRUD
type ProfileHandler struct {
	userRepo *repository.UserRepo
}

func NewProfileHandler(userRepo *repository.UserRepo) *ProfileHandler {
	return &ProfileHandler{userRepo: userRepo}
}

// GetProfile handles GET /profile
func (h *ProfileHandler) GetProfile(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	user, err := h.userRepo.FindByID(c.Request.Context(), userID)
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// UpdateProfile handles PUT /profile
func (h *ProfileHandler) UpdateProfile(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var updates model.User
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	updated, err := h.userRepo.Update(c.Request.Context(), userID, &updates)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update profile")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update profile"})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// UpdateSkills handles PUT /profile/skills
func (h *ProfileHandler) UpdateSkills(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var req struct {
		Skills []string `json:"skills"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if err := h.userRepo.UpdateSkills(c.Request.Context(), userID, req.Skills); err != nil {
		log.Error().Err(err).Msg("Failed to update skills")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update skills"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"skills": req.Skills})
}

// getUserID extracts and parses the user UUID from context
func getUserID(c *gin.Context) (uuid.UUID, error) {
	idStr := middleware.GetUserID(c)
	return uuid.Parse(idStr)
}
