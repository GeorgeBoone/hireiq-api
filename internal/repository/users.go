package repository

import (
	"context"
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

// FindByFirebaseUID looks up a user by their Firebase UID
func (r *UserRepo) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*model.User, error) {
	var u model.User
	err := r.pool.QueryRow(ctx, `
		SELECT id, firebase_uid, email, name, bio, location, work_style,
		       salary_min, salary_max, skills, github_url, created_at, updated_at
		FROM users
		WHERE firebase_uid = $1
	`, firebaseUID).Scan(
		&u.ID, &u.FirebaseUID, &u.Email, &u.Name, &u.Bio, &u.Location,
		&u.WorkStyle, &u.SalaryMin, &u.SalaryMax, &u.Skills, &u.GithubURL,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding user by firebase uid: %w", err)
	}
	return &u, nil
}

// FindByID looks up a user by internal UUID
func (r *UserRepo) FindByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	var u model.User
	err := r.pool.QueryRow(ctx, `
		SELECT id, firebase_uid, email, name, bio, location, work_style,
		       salary_min, salary_max, skills, github_url, created_at, updated_at
		FROM users
		WHERE id = $1
	`, id).Scan(
		&u.ID, &u.FirebaseUID, &u.Email, &u.Name, &u.Bio, &u.Location,
		&u.WorkStyle, &u.SalaryMin, &u.SalaryMax, &u.Skills, &u.GithubURL,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding user by id: %w", err)
	}
	return &u, nil
}

// Create inserts a new user
func (r *UserRepo) Create(ctx context.Context, firebaseUID, email, name string) (*model.User, error) {
	var u model.User
	err := r.pool.QueryRow(ctx, `
		INSERT INTO users (firebase_uid, email, name, skills)
		VALUES ($1, $2, $3, '{}')
		RETURNING id, firebase_uid, email, name, bio, location, work_style,
		          salary_min, salary_max, skills, github_url, created_at, updated_at
	`, firebaseUID, email, name).Scan(
		&u.ID, &u.FirebaseUID, &u.Email, &u.Name, &u.Bio, &u.Location,
		&u.WorkStyle, &u.SalaryMin, &u.SalaryMax, &u.Skills, &u.GithubURL,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}
	return &u, nil
}

// Update updates a user's profile fields
func (r *UserRepo) Update(ctx context.Context, id uuid.UUID, updates *model.User) (*model.User, error) {
	var u model.User
	err := r.pool.QueryRow(ctx, `
		UPDATE users
		SET name = $2, bio = $3, location = $4, work_style = $5,
		    salary_min = $6, salary_max = $7, github_url = $8, updated_at = now()
		WHERE id = $1
		RETURNING id, firebase_uid, email, name, bio, location, work_style,
		          salary_min, salary_max, skills, github_url, created_at, updated_at
	`, id, updates.Name, updates.Bio, updates.Location, updates.WorkStyle,
		updates.SalaryMin, updates.SalaryMax, updates.GithubURL,
	).Scan(
		&u.ID, &u.FirebaseUID, &u.Email, &u.Name, &u.Bio, &u.Location,
		&u.WorkStyle, &u.SalaryMin, &u.SalaryMax, &u.Skills, &u.GithubURL,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("updating user: %w", err)
	}
	return &u, nil
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
