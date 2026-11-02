// Package project defines the project resource and the store that persists it.
package project

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
)

const (
	maxNameLen        = 80
	maxSlugLen        = 80
	maxDescriptionLen = 500
)

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid")
)

// Error is a store error that carries a client-safe message and unwraps to
// one of the sentinel errors.
type Error struct {
	Sentinel error
	Message  string
}

// Error implements the error interface.
func (e *Error) Error() string { return e.Message }

// Unwrap returns the sentinel this error represents.
func (e *Error) Unwrap() error { return e.Sentinel }

// Project is an Oktant project, the unit that owns a repository and its
// build, deploy, and observe activity.
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// CreateInput is the data required to create a project.
//
// Slug is optional. When it is empty, the store derives one from Name.
type CreateInput struct {
	Name        string
	Slug        string
	Description string
}

// UpdateInput patches a project. Nil fields are left unchanged.
type UpdateInput struct {
	Name        *string
	Slug        *string
	Description *string
}

// Store persists projects.
//
// The memory store is the scaffold implementation. A database-backed store
// should implement this same interface.
type Store interface {
	Create(ctx context.Context, in CreateInput) (Project, error)
	Get(ctx context.Context, id string) (Project, error)
	List(ctx context.Context) ([]Project, error)
	Update(ctx context.Context, id string, in UpdateInput) (Project, error)
	Delete(ctx context.Context, id string) error
}

func invalid(message string) error {
	return &Error{Sentinel: ErrInvalid, Message: message}
}

func conflict(message string) error {
	return &Error{Sentinel: ErrConflict, Message: message}
}

func normalizeCreate(in CreateInput) (CreateInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Slug = strings.TrimSpace(in.Slug)

	if in.Name == "" {
		return CreateInput{}, invalid("name is required")
	}
	if len(in.Name) > maxNameLen {
		return CreateInput{}, invalid("name is too long")
	}
	if len(in.Description) > maxDescriptionLen {
		return CreateInput{}, invalid("description is too long")
	}
	if in.Slug == "" {
		in.Slug = slugify(in.Name)
	}
	if err := validateSlug(in.Slug); err != nil {
		return CreateInput{}, err
	}
	return in, nil
}

func validateSlug(slug string) error {
	if slug == "" {
		return invalid("slug is required")
	}
	if len(slug) > maxSlugLen {
		return invalid("slug is too long")
	}
	for i, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0 && i < len(slug)-1:
		default:
			return invalid("slug must be lowercase letters, digits, and single hyphens")
		}
	}
	if strings.Contains(slug, "--") {
		return invalid("slug must be lowercase letters, digits, and single hyphens")
	}
	return nil
}

func slugify(name string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		if !prevHyphen && b.Len() > 0 {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}
