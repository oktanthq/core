package project

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// MemoryStore is a process-local Store safe for concurrent use.
//
// Data is discarded when the process exits. It exists so the HTTP API can
// run before a database is attached.
type MemoryStore struct {
	mu       sync.Mutex
	order    []string
	projects map[string]Project
	bySlug   map[string]string
	now      func() time.Time
}

// NewMemoryStore returns an empty memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		projects: map[string]Project{},
		bySlug:   map[string]string{},
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Create inserts a project. Slugs are unique.
func (s *MemoryStore) Create(_ context.Context, in CreateInput) (Project, error) {
	normalized, err := normalizeCreate(in)
	if err != nil {
		return Project{}, err
	}

	id, err := newID()
	if err != nil {
		return Project{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, taken := s.bySlug[normalized.Slug]; taken {
		return Project{}, conflict("slug is already in use")
	}

	now := s.now()
	project := Project{
		ID:          id,
		Name:        normalized.Name,
		Slug:        normalized.Slug,
		Description: normalized.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.projects[id] = project
	s.bySlug[project.Slug] = id
	s.order = append(s.order, id)
	return project, nil
}

// Get returns the project with the given id.
func (s *MemoryStore) Get(_ context.Context, id string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, ok := s.projects[id]
	if !ok {
		return Project{}, &Error{Sentinel: ErrNotFound, Message: "project not found"}
	}
	return project, nil
}

// List returns projects in creation order.
func (s *MemoryStore) List(context.Context) ([]Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Project, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.projects[id])
	}
	return out, nil
}

// Update applies a partial update. Slugs stay unique.
func (s *MemoryStore) Update(_ context.Context, id string, in UpdateInput) (Project, error) {
	if in.Name == nil && in.Slug == nil && in.Description == nil {
		return Project{}, invalid("no fields to update")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.projects[id]
	if !ok {
		return Project{}, &Error{Sentinel: ErrNotFound, Message: "project not found"}
	}

	next := current
	if in.Name != nil {
		next.Name = strings.TrimSpace(*in.Name)
		if next.Name == "" {
			return Project{}, invalid("name is required")
		}
		if len(next.Name) > maxNameLen {
			return Project{}, invalid("name is too long")
		}
	}
	if in.Description != nil {
		next.Description = strings.TrimSpace(*in.Description)
		if len(next.Description) > maxDescriptionLen {
			return Project{}, invalid("description is too long")
		}
	}
	if in.Slug != nil {
		next.Slug = strings.TrimSpace(*in.Slug)
		if err := validateSlug(next.Slug); err != nil {
			return Project{}, err
		}
	}

	if owner, taken := s.bySlug[next.Slug]; taken && owner != id {
		return Project{}, conflict("slug is already in use")
	}

	if next.Slug != current.Slug {
		delete(s.bySlug, current.Slug)
		s.bySlug[next.Slug] = id
	}
	next.UpdatedAt = s.now()
	s.projects[id] = next
	return next, nil
}

// Delete removes a project.
func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.projects[id]
	if !ok {
		return &Error{Sentinel: ErrNotFound, Message: "project not found"}
	}
	delete(s.projects, id)
	delete(s.bySlug, current.Slug)
	next := make([]string, 0, len(s.order))
	for _, existing := range s.order {
		if existing != id {
			next = append(next, existing)
		}
	}
	s.order = next
	return nil
}

func newID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "prj_" + hex.EncodeToString(buf[:]), nil
}
