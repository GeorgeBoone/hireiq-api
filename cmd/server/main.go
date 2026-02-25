package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/yourusername/hireiq-api/internal/config"
	"github.com/yourusername/hireiq-api/internal/handler"
	"github.com/yourusername/hireiq-api/internal/middleware"
	"github.com/yourusername/hireiq-api/internal/repository"
)

func main() {
	// ── Logging ──────────────────────────────────────────
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if os.Getenv("ENV") == "development" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	// ── Config ───────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}
	log.Info().Str("env", cfg.Env).Str("port", cfg.Port).Msg("Starting HireIQ API")

	// ── Database ─────────────────────────────────────────
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to ping database")
	}
	log.Info().Msg("Database connected")

	// ── Repositories ─────────────────────────────────────
	userRepo := repository.NewUserRepo(pool)
	jobRepo := repository.NewJobRepo(pool)
	appRepo := repository.NewApplicationRepo(pool)
	noteRepo := repository.NewNoteRepo(pool)
	contactRepo := repository.NewContactRepo(pool)

	// ── Handlers ─────────────────────────────────────────
	authHandler := handler.NewAuthHandler(userRepo)
	profileHandler := handler.NewProfileHandler(userRepo)
	jobHandler := handler.NewJobHandler(jobRepo)
	_ = appRepo     // Will be used by application handler
	_ = noteRepo    // Will be used by notes handler
	_ = contactRepo // Will be used by contacts handler

	// ── Middleware ────────────────────────────────────────
	authMiddleware, err := middleware.NewAuthMiddleware(cfg.FirebaseProjectID)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize Firebase auth")
	}
	rateLimiter := middleware.NewRateLimiter(cfg.RateLimitRPS)

	// ── Router ───────────────────────────────────────────
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestLogger())

	// CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Authorization", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Health check (unauthenticated)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "hireiq-api",
			"time":    time.Now().UTC(),
		})
	})

	// ── Authenticated Routes ─────────────────────────────
	api := r.Group("/", authMiddleware.Authenticate(), rateLimiter.Limit())
	{
		// After auth middleware verifies Firebase token, resolve internal user ID
		api.Use(resolveUserID(userRepo))

		// Auth
		api.POST("/auth/google", authHandler.GoogleSignIn)

		// Profile
		api.GET("/profile", profileHandler.GetProfile)
		api.PUT("/profile", profileHandler.UpdateProfile)
		api.PUT("/profile/skills", profileHandler.UpdateSkills)

		// Jobs
		api.GET("/jobs", jobHandler.ListJobs)
		api.POST("/jobs", jobHandler.CreateJob)
		api.GET("/jobs/:id", jobHandler.GetJob)
		api.PUT("/jobs/:id", jobHandler.UpdateJob)
		api.DELETE("/jobs/:id", jobHandler.DeleteJob)
		api.POST("/jobs/:id/bookmark", jobHandler.ToggleBookmark)

		// Applications (TODO: implement handlers)
		// api.GET("/jobs/:id/application", appHandler.Get)
		// api.POST("/jobs/:id/application", appHandler.Create)
		// api.PUT("/jobs/:id/application", appHandler.UpdateStatus)

		// Notes (TODO: implement handlers)
		// api.GET("/jobs/:id/notes", noteHandler.List)
		// api.POST("/jobs/:id/notes", noteHandler.Create)
		// api.DELETE("/jobs/:id/notes/:noteId", noteHandler.Delete)

		// Contacts (TODO: implement handlers)
		// api.GET("/contacts", contactHandler.List)
		// api.POST("/contacts", contactHandler.Create)
		// api.PUT("/contacts/:id", contactHandler.Update)
		// api.DELETE("/contacts/:id", contactHandler.Delete)

		// AI (TODO: implement handlers)
		// api.POST("/ai/compare", aiHandler.Compare)
		// api.POST("/ai/fix-suggestion", aiHandler.FixSuggestion)

		// Resume (TODO: implement handlers)
		// api.POST("/resume/upload", resumeHandler.Upload)
		// api.POST("/resume/critique", resumeHandler.Critique)
		// api.GET("/resume/latest", resumeHandler.Latest)

		// Dashboard (TODO: implement handlers)
		// api.GET("/dashboard/summary", dashHandler.Summary)
		// api.GET("/dashboard/calendar", dashHandler.Calendar)
	}

	// ── Server ───────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed")
		}
	}()

	log.Info().Str("port", cfg.Port).Msg("HireIQ API server running")

	// Wait for interrupt
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("Server forced to shutdown")
	}

	log.Info().Msg("Server stopped")
}

// resolveUserID maps Firebase UID to internal user UUID for all subsequent handlers
func resolveUserID(userRepo *repository.UserRepo) gin.HandlerFunc {
	return func(c *gin.Context) {
		firebaseUID := middleware.GetFirebaseUID(c)
		if firebaseUID == "" {
			c.Next()
			return
		}

		user, err := userRepo.FindByFirebaseUID(c.Request.Context(), firebaseUID)
		if err != nil {
			log.Error().Err(err).Msg("Failed to resolve user ID")
			c.Next()
			return
		}
		if user != nil {
			c.Set(middleware.ContextKeyUserID, user.ID.String())
		}

		c.Next()
	}
}

// requestLogger logs every request with zerolog
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		event := log.Info()
		if status >= 400 {
			event = log.Warn()
		}
		if status >= 500 {
			event = log.Error()
		}

		event.
			Str("method", c.Request.Method).
			Str("path", path).
			Int("status", status).
			Dur("latency", latency).
			Str("ip", c.ClientIP()).
			Msg(fmt.Sprintf("%s %s", c.Request.Method, path))
	}
}
