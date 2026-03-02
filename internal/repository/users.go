package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourusername/hireiq-api/internal/model"
)

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

// userColumns is the shared column list for all user queries
const userColumns = `id, firebase_uid, email, name, bio, location, work_style,
       salary_min, salary_max, skills, target_roles, github_url,
       experience, education, certifications, languages, volunteer,
       created_at, updated_at`

// scanUser scans a row into a model.User, handling JSONB decoding
func scanUser(row pgx.Row) (*model.User, error) {
	var u model.User
	var expJSON, eduJSON, certJSON, langJSON, volJSON []byte

	err := row.Scan(
		&u.ID, &u.FirebaseUID, &u.Email, &u.Name, &u.Bio, &u.Location,
		&u.WorkStyle, &u.SalaryMin, &u.SalaryMax, &u.Skills, &u.TargetRoles, &u.GithubURL,
		&expJSON, &eduJSON, &certJSON, &langJSON, &volJSON,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Decode JSONB columns (default to empty slices)
	if len(expJSON) > 0 {
		json.Unmarshal(expJSON, &u.Experience)
	}
	if len(eduJSON) > 0 {
		json.Unmarshal(eduJSON, &u.Education)
	}
	if len(certJSON) > 0 {
		json.Unmarshal(certJSON, &u.Certifications)
	}
	if len(langJSON) > 0 {
		json.Unmarshal(langJSON, &u.Languages)
	}
	if len(volJSON) > 0 {
		json.Unmarshal(volJSON, &u.Volunteer)
	}

	// Ensure nil slices become empty slices for JSON serialization
	if u.TargetRoles == nil {
		u.TargetRoles = []string{}
	}
	if u.Experience == nil {
		u.Experience = []model.Experience{}
	}
	if u.Education == nil {
		u.Education = []model.Education{}
	}
	if u.Certifications == nil {
		u.Certifications = []model.Certification{}
	}
	if u.Languages == nil {
		u.Languages = []model.Language{}
	}
	if u.Volunteer == nil {
		u.Volunteer = []model.Volunteer{}
	}

	return &u, nil
}

// FindByFirebaseUID looks up a user by their Firebase UID
func (r *UserRepo) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*model.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+userColumns+`
		FROM users
		WHERE firebase_uid = $1
	`, firebaseUID)

	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding user by firebase uid: %w", err)
	}
	return u, nil
}

// FindByID looks up a user by internal UUID
func (r *UserRepo) FindByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+userColumns+`
		FROM users
		WHERE id = $1
	`, id)

	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding user by id: %w", err)
	}
	return u, nil
}

// Create inserts a new user
func (r *UserRepo) Create(ctx context.Context, firebaseUID, email, name string) (*model.User, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (firebase_uid, email, name, skills)
		VALUES ($1, $2, $3, '{}')
		RETURNING `+userColumns+`
	`, firebaseUID, email, name)

	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}
	return u, nil
}

// Update updates a user's profile fields
func (r *UserRepo) Update(ctx context.Context, id uuid.UUID, updates *model.User) (*model.User, error) {
	expJSON, _ := json.Marshal(updates.Experience)
	eduJSON, _ := json.Marshal(updates.Education)
	certJSON, _ := json.Marshal(updates.Certifications)
	langJSON, _ := json.Marshal(updates.Languages)
	volJSON, _ := json.Marshal(updates.Volunteer)

	row := r.pool.QueryRow(ctx, `
		UPDATE users
		SET name = $2, bio = $3, location = $4, work_style = $5,
		    salary_min = $6, salary_max = $7, target_roles = $8, github_url = $9,
		    experience = $10, education = $11, certifications = $12,
		    languages = $13, volunteer = $14,
		    updated_at = now()
		WHERE id = $1
		RETURNING `+userColumns+`
	`, id, updates.Name, updates.Bio, updates.Location, updates.WorkStyle,
		updates.SalaryMin, updates.SalaryMax, updates.TargetRoles, updates.GithubURL,
		expJSON, eduJSON, certJSON, langJSON, volJSON,
	)

	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("updating user: %w", err)
	}
	return u, nil
}

// UpdateSkills replaces the user's skills array
func (r *UserRepo) UpdateSkills(ctx context.Context, id uuid.UUID, skills []string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE users SET skills = $2, updated_at = now() WHERE id = $1
	`, id, skills)
	if err != nil {
		return fmt.Errorf("updating skills: %w", err)
	}
	return nil
}
