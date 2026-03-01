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
	"github.com/yourusername/hireiq-api/internal/service"
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
	feedRepo := repository.NewFeedRepo(pool)
	stripeCustomerRepo := repository.NewStripeCustomerRepo(pool)
	subscriptionRepo := repository.NewSubscriptionRepo(pool)

	// ── Services ──────────────────────────────────────────
	claudeClient := service.NewClaudeClient(cfg.ClaudeAPIKey, cfg.ClaudeBaseURL)
	yahooClient := service.NewYahooFinanceClient()
	jsearchClient := service.NewJSearchClient(cfg.RapidAPIKey)
	feedService := service.NewFeedService(jsearchClient, feedRepo, userRepo)
	stripeService := service.NewStripeService(cfg, stripeCustomerRepo, subscriptionRepo, userRepo)

	// ── Handlers ─────────────────────────────────────────
	resumeHandler := handler.NewResumeHandler(claudeClient, jobRepo)
	authHandler := handler.NewAuthHandler(userRepo)
	profileHandler := handler.NewProfileHandler(userRepo)
	jobHandler := handler.NewJobHandler(jobRepo, appRepo)
	parseHandler := handler.NewParseHandler(claudeClient)
	feedHandler := handler.NewFeedHandler(feedService, feedRepo)
	companyHandler := handler.NewCompanyHandler(yahooClient, claudeClient)
	compareHandler := handler.NewCompareHandler(claudeClient, jobRepo, userRepo)
	appHandler := handler.NewApplicationHandler(appRepo, jobRepo)
	contactHandler := handler.NewContactHandler(contactRepo)
	networkHandler := handler.NewNetworkHandler(jobRepo, contactRepo)
	billingHandler := handler.NewBillingHandler(stripeService, subscriptionRepo)
	_ = noteRepo // Will be used by notes handler

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
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
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

	// Stripe webhook (unauthenticated — verified by Stripe signature)
	r.POST("/billing/webhook", billingHandler.HandleWebhook)

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

		// Billing (subscription management)
		api.GET("/billing/subscription", billingHandler.GetSubscription)
		api.POST("/billing/checkout", billingHandler.CreateCheckout)
		api.POST("/billing/portal", billingHandler.CreatePortal)

		// Jobs
		api.GET("/jobs", jobHandler.ListJobs)
		api.POST("/jobs", jobHandler.CreateJob)
		api.GET("/jobs/:id", jobHandler.GetJob)
		api.PUT("/jobs/:id", jobHandler.UpdateJob)
		api.DELETE("/jobs/:id", jobHandler.DeleteJob)
		api.POST("/jobs/:id/bookmark", jobHandler.ToggleBookmark)
		api.PATCH("/jobs/:id/status", jobHandler.UpdateJobStatus)

		// Feed (discover)
		api.GET("/feed", feedHandler.GetFeed)
		api.POST("/feed/refresh", feedHandler.RefreshFeed)
		api.POST("/feed/:id/dismiss", feedHandler.DismissFeedJob)
		api.POST("/feed/:id/save", feedHandler.SaveFeedJob)

		// Applications (pipeline tracking)
		api.GET("/jobs/:id/application", appHandler.Get)
		api.POST("/jobs/:id/application", appHandler.Create)
		api.PUT("/jobs/:id/application/status", appHandler.UpdateStatus)
		api.PUT("/jobs/:id/application/details", appHandler.UpdateDetails)
		api.GET("/jobs/:id/application/history", appHandler.GetHistory)

		// Notes (TODO: implement handlers)
		// api.GET("/jobs/:id/notes", noteHandler.List)
		// api.POST("/jobs/:id/notes", noteHandler.Create)
		// api.DELETE("/jobs/:id/notes/:noteId", noteHandler.Delete)

		// Contacts
		api.GET("/contacts", contactHandler.List)
		api.POST("/contacts", contactHandler.Create)
		api.PUT("/contacts/:id", contactHandler.Update)
		api.DELETE("/contacts/:id", contactHandler.Delete)

		// Network (company aggregation)
		api.GET("/network/companies", networkHandler.ListCompanies)
		api.GET("/network/companies/:company/detail", networkHandler.GetCompanyDetail)

		// ── Pro+ features (require Pro plan) ─────────────
		requirePro := middleware.RequirePlan("pro", subscriptionRepo)

		api.POST("/jobs/parse", requirePro, parseHandler.ParseJobPosting)
		api.POST("/ai/compare", requirePro, compareHandler.Compare)
		api.GET("/company/intel", requirePro, companyHandler.GetIntel)

		// Resume
		api.POST("/resume/upload", resumeHandler.Upload)
		api.POST("/resume/critique", requirePro, resumeHandler.Critique)
		api.POST("/resume/fix", requirePro, resumeHandler.Fix)

		// Dashboard (TODO: implement handlers)
		// api.GET("/dashboard/summary", dashHandler.Summary)
		// api.GET("/dashboard/calendar", dashHandler.Calendar)
	}

	// ── Server ───────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
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
