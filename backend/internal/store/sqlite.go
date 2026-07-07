package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("project not found")

type SQLite struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLite, error) {
	if path == "" {
		path = "./data/app.db"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	store := &SQLite{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLite) Close() error {
	return s.db.Close()
}

func (s *SQLite) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	owner_id TEXT NOT NULL DEFAULT 'default',
	title TEXT NOT NULL,
	prompt TEXT NOT NULL,
	code TEXT NOT NULL,
	language TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_projects_updated_at ON projects(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_projects_owner_updated_at ON projects(owner_id, updated_at DESC);
CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE projects ADD COLUMN owner_id TEXT NOT NULL DEFAULT 'default'`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	_, err = s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_projects_owner_updated_at ON projects(owner_id, updated_at DESC)`)
	return err
}

func (s *SQLite) ListProjects(ctx context.Context) ([]Project, error) {
	return s.ListProjectsForOwner(ctx, "default")
}

func (s *SQLite) ListProjectsForOwner(ctx context.Context, ownerID string) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, owner_id, title, prompt, code, language, created_at, updated_at
FROM projects
WHERE owner_id = ?
ORDER BY updated_at DESC
`, normalizeOwnerID(ownerID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := make([]Project, 0)
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *SQLite) GetProject(ctx context.Context, id string) (Project, error) {
	return s.GetProjectForOwner(ctx, "default", id)
}

func (s *SQLite) GetProjectForOwner(ctx context.Context, ownerID string, id string) (Project, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, owner_id, title, prompt, code, language, created_at, updated_at
FROM projects
WHERE owner_id = ? AND id = ?
`, normalizeOwnerID(ownerID), id)
	project, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return project, err
}

func (s *SQLite) CreateProject(ctx context.Context, input ProjectInput) (Project, error) {
	return s.CreateProjectForOwner(ctx, input.OwnerID, input)
}

func (s *SQLite) CreateProjectForOwner(ctx context.Context, ownerID string, input ProjectInput) (Project, error) {
	now := time.Now().UTC()
	project := Project{
		ID:        newID(),
		OwnerID:   normalizeOwnerID(ownerID),
		Title:     nonEmpty(input.Title, titleFromPrompt(input.Prompt)),
		Prompt:    strings.TrimSpace(input.Prompt),
		Code:      strings.TrimSpace(input.Code),
		Language:  nonEmpty(input.Language, "cascade-js"),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if project.Title == "" {
		project.Title = "Untitled CAD Project"
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO projects (id, owner_id, title, prompt, code, language, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, project.ID, project.OwnerID, project.Title, project.Prompt, project.Code, project.Language, formatTime(project.CreatedAt), formatTime(project.UpdatedAt))
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

func (s *SQLite) UpdateProject(ctx context.Context, id string, input ProjectInput) (Project, error) {
	return s.UpdateProjectForOwner(ctx, "default", id, input)
}

func (s *SQLite) UpdateProjectForOwner(ctx context.Context, ownerID string, id string, input ProjectInput) (Project, error) {
	ownerID = normalizeOwnerID(ownerID)
	existing, err := s.GetProjectForOwner(ctx, ownerID, id)
	if err != nil {
		return Project{}, err
	}

	existing.Title = nonEmpty(input.Title, existing.Title)
	existing.Prompt = input.Prompt
	existing.Code = input.Code
	existing.Language = nonEmpty(input.Language, existing.Language)
	existing.UpdatedAt = time.Now().UTC()

	result, err := s.db.ExecContext(ctx, `
UPDATE projects
SET title = ?, prompt = ?, code = ?, language = ?, updated_at = ?
WHERE owner_id = ? AND id = ?
`, existing.Title, existing.Prompt, existing.Code, existing.Language, formatTime(existing.UpdatedAt), ownerID, id)
	if err != nil {
		return Project{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Project{}, err
	}
	if affected == 0 {
		return Project{}, ErrNotFound
	}
	return existing, nil
}

func (s *SQLite) DeleteProject(ctx context.Context, id string) error {
	return s.DeleteProjectForOwner(ctx, "default", id)
}

func (s *SQLite) DeleteProjectForOwner(ctx context.Context, ownerID string, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE owner_id = ? AND id = ?`, normalizeOwnerID(ownerID), id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

type projectScanner interface {
	Scan(dest ...any) error
}

func scanProject(scanner projectScanner) (Project, error) {
	var project Project
	var createdAt, updatedAt string
	if err := scanner.Scan(&project.ID, &project.OwnerID, &project.Title, &project.Prompt, &project.Code, &project.Language, &createdAt, &updatedAt); err != nil {
		return Project{}, err
	}

	parsedCreatedAt, err := parseTime(createdAt)
	if err != nil {
		return Project{}, err
	}
	parsedUpdatedAt, err := parseTime(updatedAt)
	if err != nil {
		return Project{}, err
	}
	project.CreatedAt = parsedCreatedAt
	project.UpdatedAt = parsedUpdatedAt
	return project, nil
}

func normalizeOwnerID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	if len(value) > 128 {
		return value[:128]
	}
	return value
}

func newID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

func nonEmpty(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return strings.TrimSpace(fallback)
	}
	return value
}

func titleFromPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	runes := []rune(prompt)
	if len(runes) > 24 {
		return string(runes[:24]) + "..."
	}
	return prompt
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
