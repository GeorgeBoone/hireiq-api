package model

import (
	"time"

	"github.com/google/uuid"
)

// ── Profile section types ──────────────────────────────

type Experience struct {
	Title       string `json:"title"`
	Company     string `json:"company"`
	Location    string `json:"location"`
	StartDate   string `json:"startDate"`
	EndDate     string `json:"endDate"`
	Current     bool   `json:"current"`
	Description string `json:"description"`
}

type Education struct {
	School    string `json:"school"`
	Degree    string `json:"degree"`
	Field     string `json:"field"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

type Certification struct {
	Name         string `json:"name"`
	Issuer       string `json:"issuer"`
	DateObtained string `json:"dateObtained"`
	ExpiryDate   string `json:"expiryDate,omitempty"`
	CredentialId string `json:"credentialId,omitempty"`
}

type Language struct {
	Language    string `json:"language"`
	Proficiency string `json:"proficiency"`
}

type Volunteer struct {
	Organization string `json:"organization"`
	Role         string `json:"role"`
	StartDate    string `json:"startDate"`
	EndDate      string `json:"endDate"`
	Description  string `json:"description"`
}

// User represents a HireIQ user profile
type User struct {
	ID             uuid.UUID       `json:"id"`
	FirebaseUID    string          `json:"-"`
	Email          string          `json:"email"`
	Name           string          `json:"name"`
	Bio            string          `json:"bio"`
	Location       string          `json:"location"`
	WorkStyle      string          `json:"workStyle"`
	SalaryMin      int             `json:"salaryMin"`
	SalaryMax      int             `json:"salaryMax"`
	Skills         []string        `json:"skills"`
	TargetRoles    []string        `json:"targetRoles"`
	GithubURL      string          `json:"githubUrl"`
	Experience     []Experience    `json:"experience"`
	Education      []Education     `json:"education"`
	Certifications []Certification `json:"certifications"`
	Languages      []Language      `json:"languages"`
	Volunteer      []Volunteer     `json:"volunteer"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

// Job represents a saved/tracked job listing
type Job struct {
	ID              uuid.UUID  `json:"id"`
	UserID          uuid.UUID  `json:"userId"`
	ExternalID      string     `json:"externalId,omitempty"`
	Source          string     `json:"source"`
	Title           string     `json:"title"`
	Company         string     `json:"company"`
	Location        string     `json:"location"`
	SalaryRange     string     `json:"salaryRange"`
	JobType         string     `json:"jobType"`
	Description     string     `json:"description"`
	Tags            []string   `json:"tags"`
	RequiredSkills  []string   `json:"requiredSkills"`
	PreferredSkills []string   `json:"preferredSkills"`
	ApplyURL        string     `json:"applyUrl,omitempty"`
	HiringEmail     string     `json:"hiringEmail,omitempty"`
	CompanyLogo     string     `json:"companyLogo,omitempty"`
	CompanyColor    string     `json:"companyColor,omitempty"`
	MatchScore      int        `json:"matchScore"`
	Bookmarked      bool       `json:"bookmarked"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

// Application represents a job application pipeline entry
type Application struct {
	ID             uuid.UUID  `json:"id"`
	UserID         uuid.UUID  `json:"userId"`
	JobID          uuid.UUID  `json:"jobId"`
	Status         string     `json:"status"`
	AppliedAt      *time.Time `json:"appliedAt,omitempty"`
	NextStep       string     `json:"nextStep,omitempty"`
	FollowUpDate   *time.Time `json:"followUpDate,omitempty"`
	FollowUpType   string     `json:"followUpType,omitempty"`
	FollowUpUrgent bool       `json:"followUpUrgent"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`

	// Joined data (populated by service layer)
	Job            *Job       `json:"job,omitempty"`
}

// Valid application statuses
const (
	StatusSaved      = "saved"
	StatusApplied    = "applied"
	StatusScreening  = "screening"
	StatusInterview  = "interview"
	StatusOffer      = "offer"
	StatusRejected   = "rejected"
	StatusWithdrawn  = "withdrawn"
)

func ValidStatus(s string) bool {
	switch s {
	case StatusSaved, StatusApplied, StatusScreening, StatusInterview,
		StatusOffer, StatusRejected, StatusWithdrawn:
		return true
	}
	return false
}

// StatusHistory tracks application stage changes for timeline
type StatusHistory struct {
	ID            uuid.UUID  `json:"id"`
	ApplicationID uuid.UUID  `json:"applicationId"`
	FromStatus    string     `json:"fromStatus"`
	ToStatus      string     `json:"toStatus"`
	ChangedAt     time.Time  `json:"changedAt"`
	Note          string     `json:"note,omitempty"`
}

// Note represents a per-job note
type Note struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"userId"`
	JobID     uuid.UUID `json:"jobId"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

// Contact represents a networking contact
type Contact struct {
	ID           uuid.UUID       `json:"id"`
	UserID       uuid.UUID       `json:"userId"`
	Name         string          `json:"name"`
	Company      string          `json:"company"`
	Role         string          `json:"role"`
	Connection   string          `json:"connection"`
	Phone        string          `json:"phone"`
	Email        string          `json:"email"`
	Tip          string          `json:"tip"`
	Enriched     bool            `json:"enriched"`
	EnrichedData *map[string]any `json:"enrichedData,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

// Resume represents an uploaded resume
type Resume struct {
	ID             uuid.UUID       `json:"id"`
	UserID         uuid.UUID       `json:"userId"`
	Filename       string          `json:"filename"`
	RawText        string          `json:"rawText"`
	FileURL        string          `json:"fileUrl"`
	CritiqueResult *map[string]any `json:"critiqueResult,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// CompetitiveSnapshot holds aggregated positioning data
type CompetitiveSnapshot struct {
	ID             uuid.UUID `json:"id"`
	JobID          uuid.UUID `json:"jobId"`
	SnapshotDate   time.Time `json:"snapshotDate"`
	ApplicantCount int       `json:"applicantCount"`
	AvgMatchScore  int       `json:"avgMatchScore"`
	Trend          string    `json:"trend"`
	CreatedAt      time.Time `json:"createdAt"`
}

// FeedJob represents a cached job listing from external APIs
type FeedJob struct {
	ID             uuid.UUID  `json:"id"`
	ExternalID     string     `json:"externalId"`
	Source         string     `json:"source"`
	Title          string     `json:"title"`
	Company        string     `json:"company"`
	Location       string     `json:"location"`
	SalaryMin      int        `json:"salaryMin"`
	SalaryMax      int        `json:"salaryMax"`
	SalaryText     string     `json:"salaryText"`
	JobType        string     `json:"jobType"`
	Description    string     `json:"description"`
	RequiredSkills []string   `json:"requiredSkills"`
	ApplyURL       string     `json:"applyUrl"`
	CompanyLogo    string     `json:"companyLogo"`
	PostedAt       *time.Time `json:"postedAt,omitempty"`
	FetchedAt      time.Time  `json:"fetchedAt"`

	// Per-user fields (populated from user_feed join)
	MatchScore     int        `json:"matchScore"`
	Dismissed      bool       `json:"dismissed"`
	Saved          bool       `json:"saved"`
	SavedJobID     *uuid.UUID `json:"savedJobId,omitempty"`
}

// UserFeed links a user to a feed job with personalized data
type UserFeed struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"userId"`
	FeedJobID  uuid.UUID  `json:"feedJobId"`
	MatchScore int        `json:"matchScore"`
	Dismissed  bool       `json:"dismissed"`
	Saved      bool       `json:"saved"`
	SavedJobID *uuid.UUID `json:"savedJobId,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// DashboardSummary is the aggregated response for the home tab
type DashboardSummary struct {
	PipelineCounts  map[string]int   `json:"pipelineCounts"`
	UpcomingEvents  []CalendarEvent  `json:"upcomingEvents"`
	TopMatches      []Job            `json:"topMatches"`
	RecentNotes     []NoteWithJob    `json:"recentNotes"`
	ContactStats    ContactStats     `json:"contactStats"`
}

type CalendarEvent struct {
	Date         time.Time `json:"date"`
	Type         string    `json:"type"`
	Company      string    `json:"company"`
	JobTitle     string    `json:"jobTitle"`
	Status       string    `json:"status"`
	Urgent       bool      `json:"urgent"`
}

type NoteWithJob struct {
	Note
	JobTitle string `json:"jobTitle"`
	Company  string `json:"company"`
}

type ContactStats struct {
	Total       int            `json:"total"`
	FirstDegree int            `json:"firstDegree"`
	WithEmail   int            `json:"withEmail"`
	WithPhone   int            `json:"withPhone"`
	ByCompany   map[string]int `json:"byCompany"`
}

// CompanySummary is an aggregated view of a company from the user's saved jobs
type CompanySummary struct {
	Company      string `json:"company"`
	CompanyLogo  string `json:"companyLogo"`
	CompanyColor string `json:"companyColor"`
	JobCount     int    `json:"jobCount"`
	ContactCount int    `json:"contactCount"`
}

// ── Stripe / Billing ────────────────────────────────────

// StripeCustomer links a HireIQ user to their Stripe customer record
type StripeCustomer struct {
	ID               uuid.UUID `json:"id"`
	UserID           uuid.UUID `json:"userId"`
	StripeCustomerID string    `json:"stripeCustomerId"`
	Email            string    `json:"email"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// Subscription tracks a user's active Stripe subscription
type Subscription struct {
	ID                uuid.UUID  `json:"id"`
	UserID            uuid.UUID  `json:"userId"`
	StripeSubID       string     `json:"stripeSubId,omitempty"`
	StripePriceID     string     `json:"stripePriceId,omitempty"`
	Plan              string     `json:"plan"`
	Status            string     `json:"status"`
	CurrentPeriodEnd  *time.Time `json:"currentPeriodEnd"`
	CancelAtPeriodEnd bool       `json:"cancelAtPeriodEnd"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

// Subscription plan constants
const (
	PlanFree    = "free"
	PlanPro     = "pro"
	PlanProPlus = "pro_plus"
)

// Subscription status constants
const (
	SubStatusActive   = "active"
	SubStatusPastDue  = "past_due"
	SubStatusCanceled = "canceled"
	SubStatusTrialing = "trialing"
)

// PlanLevel returns a numeric level for plan comparison (higher = more features)
func PlanLevel(plan string) int {
	switch plan {
	case PlanPro:
		return 1
	case PlanProPlus:
		return 2
	default:
		return 0
	}
}

// PaymentEvent stores a webhook event for audit
type PaymentEvent struct {
	ID               uuid.UUID `json:"id"`
	StripeEventID    string    `json:"stripeEventId"`
	EventType        string    `json:"eventType"`
	StripeCustomerID string    `json:"stripeCustomerId,omitempty"`
	Data             []byte    `json:"data"`
	Processed        bool      `json:"processed"`
	CreatedAt        time.Time `json:"createdAt"`
}
