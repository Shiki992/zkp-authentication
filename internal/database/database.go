package database

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type Database struct {
	db *sql.DB
}

// func (d *Database) GetUserByUsername(ctx context.Context, param any) (*User, error) {
// 	panic("unimplemented")
// }

type User struct {
	ID        int64
	Username  string
	Y1        *big.Int
	Y2        *big.Int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AuthSession struct {
	ID           int64
	Username     string
	AuthID       string
	UserID       int64
	ChallengeC   *big.Int
	CommitmentR1 *big.Int
	CommitmentR2 *big.Int
	CreatedAt    time.Time
	ExpiresAt    time.Time
	Verified     bool
}

type ActiveSession struct {
	ID           int64
	SessionID    string
	UserID       int64
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastActivity time.Time
}

// Config holds database configuration
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

// NewDatabase creates a new database connection
func NewDatabase(cfg Config) (*Database, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &Database{db: db}, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// RegisterUser creates a new user in the database
func (d *Database) RegisterUser(ctx context.Context, username string, y1, y2 *big.Int) error {
	query := `
		INSERT INTO users (username, y1, y2)
		VALUES ($1, $2, $3)
	`

	_, err := d.db.ExecContext(ctx, query, username, y1.String(), y2.String())
	if err != nil {
		return fmt.Errorf("failed to register user: %w", err)
	}

	return nil
}

// GetUserByUsername retrieves a user by username
func (d *Database) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	query := `
		SELECT id, username, y1, y2, created_at, updated_at
		FROM users
		WHERE username = $1
	`

	var user User
	var y1Str, y2Str string

	err := d.db.QueryRowContext(ctx, query, username).Scan(
		&user.ID,
		&user.Username,
		&y1Str,
		&y2Str,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Parse big integers
	user.Y1 = new(big.Int)
	user.Y1.SetString(y1Str, 10)
	user.Y2 = new(big.Int)
	user.Y2.SetString(y2Str, 10)

	return &user, nil
}

// GetUserByID retrieves a user by their ID
func (d *Database) GetUserByID(ctx context.Context, id int64) (*User, error) {
	query := `
		SELECT id, username, y1, y2, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	var user User
	var y1Str, y2Str string

	err := d.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.Username,
		&y1Str,
		&y2Str,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Parse big integers
	user.Y1 = new(big.Int)
	user.Y1.SetString(y1Str, 10)
	user.Y2 = new(big.Int)
	user.Y2.SetString(y2Str, 10)

	return &user, nil
}

// UserExists checks if a user exists
func (d *Database) UserExists(ctx context.Context, username string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)`

	var exists bool
	err := d.db.QueryRowContext(ctx, query, username).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check user existence: %w", err)
	}

	return exists, nil
}

// CreateAuthSession creates a new authentication session
func (d *Database) CreateAuthSession(ctx context.Context, username string, c, r1, r2 *big.Int, ttl time.Duration) (string, error) {
	// Start transaction
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get user ID
	var userID int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM users WHERE username = $1", username).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("failed to get user ID: %w", err)
	}

	// Generate auth ID
	authID := uuid.New().String()
	expiresAt := time.Now().Add(ttl)

	// Insert auth session
	query := `
		INSERT INTO auth_sessions (auth_id, user_id, challenge_c, commitment_r1, commitment_r2, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err = tx.ExecContext(ctx, query, authID, userID, c.String(), r1.String(), r2.String(), expiresAt)
	if err != nil {
		return "", fmt.Errorf("failed to create auth session: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}

	return authID, nil
}

// GetAuthSession retrieves an authentication session
func (d *Database) GetAuthSession(ctx context.Context, authID string) (*AuthSession, error) {
	query := `
		SELECT id, auth_id, user_id, challenge_c, commitment_r1, commitment_r2, 
		       created_at, expires_at, verified
		FROM auth_sessions
		WHERE auth_id = $1 AND expires_at > NOW()
	`

	var session AuthSession
	var cStr, r1Str, r2Str string

	err := d.db.QueryRowContext(ctx, query, authID).Scan(
		&session.ID,
		&session.AuthID,
		&session.UserID,
		&cStr,
		&r1Str,
		&r2Str,
		&session.CreatedAt,
		&session.ExpiresAt,
		&session.Verified,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("auth session not found or expired")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get auth session: %w", err)
	}

	// Parse big integers
	session.ChallengeC = new(big.Int)
	session.ChallengeC.SetString(cStr, 10)
	session.CommitmentR1 = new(big.Int)
	session.CommitmentR1.SetString(r1Str, 10)
	session.CommitmentR2 = new(big.Int)
	session.CommitmentR2.SetString(r2Str, 10)

	return &session, nil
}

// CreateActiveSession creates a new active session after successful verification
func (d *Database) CreateActiveSession(ctx context.Context, authID string, ttl time.Duration) (string, error) {
	// Start transaction
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get user ID from auth session and mark as verified
	var userID int64
	err = tx.QueryRowContext(ctx,
		`UPDATE auth_sessions SET verified = true 
		 WHERE auth_id = $1 RETURNING user_id`,
		authID,
	).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("failed to verify auth session: %w", err)
	}

	// Generate session ID
	sessionID := uuid.New().String()
	expiresAt := time.Now().Add(ttl)

	// Insert active session
	query := `
		INSERT INTO active_sessions (session_id, user_id, expires_at)
		VALUES ($1, $2, $3)
	`

	_, err = tx.ExecContext(ctx, query, sessionID, userID, expiresAt)
	if err != nil {
		return "", fmt.Errorf("failed to create active session: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}

	return sessionID, nil
}

// GetActiveSession retrieves an active session
func (d *Database) GetActiveSession(ctx context.Context, sessionID string) (*ActiveSession, error) {
	query := `
		SELECT id, session_id, user_id, created_at, expires_at, last_activity
		FROM active_sessions
		WHERE session_id = $1 AND expires_at > NOW()
	`

	var session ActiveSession
	err := d.db.QueryRowContext(ctx, query, sessionID).Scan(
		&session.ID,
		&session.SessionID,
		&session.UserID,
		&session.CreatedAt,
		&session.ExpiresAt,
		&session.LastActivity,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found or expired")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return &session, nil
}

// CleanupExpiredSessions removes expired sessions
func (d *Database) CleanupExpiredSessions(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, "SELECT cleanup_expired_sessions()")
	return err
}

// UpdateSessionActivity updates the last activity time for a session
func (d *Database) UpdateSessionActivity(ctx context.Context, sessionID string) error {
	query := `
		UPDATE active_sessions 
		SET last_activity = NOW() 
		WHERE session_id = $1
	`
	_, err := d.db.ExecContext(ctx, query, sessionID)
	return err
}

// DeleteSession removes an active session (logout)
func (d *Database) DeleteSession(ctx context.Context, sessionID string) error {
	query := `DELETE FROM active_sessions WHERE session_id = $1`
	_, err := d.db.ExecContext(ctx, query, sessionID)
	return err
}
