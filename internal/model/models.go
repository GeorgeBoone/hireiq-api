package model

import (
	"time"

	"github.com/google/uuid"
)

// User represents a HireIQ user profile
type User struct {
	ID           uuid.UUID  `json:"id"`
	FirebaseUID  string     `json:"-"`
	Email        string     `json:"email"`
	Name         string     `json:"name"`
	Bio          string     `json:"bio"`
	Location     string     `json:"location"`
	WorkStyle    string     `json:"workStyle"`
	SalaryMin    int        `json:"salaryMin"`
	SalaryMax    int        `json:"salaryMax"`
	Skills       []string   `json:"skills"`
	GithubURL    string     `json:"githubUrl"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
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
