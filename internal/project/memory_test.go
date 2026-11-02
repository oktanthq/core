package project

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMemoryStoreCreateAndGet(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	created, err := store.Create(ctx, CreateInput{Name: "Web Platform", Description: "public site"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(created.ID, "prj_") || created.Slug != "web-platform" {
		t.Fatalf("created = %+v", created)
	}
	if created.CreatedAt.IsZero() || !created.UpdatedAt.Equal(created.CreatedAt) {
		t.Fatalf("timestamps = %s %s", created.CreatedAt, created.UpdatedAt)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != created {
		t.Fatalf("Get = %+v, created %+v", got, created)
	}
}

func TestMemoryStoreSlugConflictAndValidation(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	if _, err := store.Create(ctx, CreateInput{Name: "API", Slug: "core"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err := store.Create(ctx, CreateInput{Name: "Other", Slug: "core"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate slug err = %v", err)
	}

	cases := []CreateInput{
		{},
		{Name: "Bad", Slug: "Not-Valid"},
		{Name: "Bad", Slug: "-leading"},
		{Name: "!!!"},
		{Name: strings.Repeat("a", maxNameLen+1)},
	}
	for _, in := range cases {
		if _, err := store.Create(ctx, in); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Create(%+v) err = %v", in, err)
		}
	}
}

func TestMemoryStoreUpdateAndDelete(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	start := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	step := 0
	store.now = func() time.Time {
		step++
		return start.Add(time.Duration(step) * time.Second)
	}

	first, err := store.Create(ctx, CreateInput{Name: "One", Slug: "one"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := store.Create(ctx, CreateInput{Name: "Two", Slug: "two"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	name := "One renamed"
	updated, err := store.Update(ctx, first.ID, UpdateInput{Name: &name})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != name || updated.Slug != "one" || !updated.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("updated = %+v", updated)
	}

	taken := "two"
	if _, err := store.Update(ctx, first.ID, UpdateInput{Slug: &taken}); !errors.Is(err, ErrConflict) {
		t.Fatalf("slug conflict err = %v", err)
	}

	if err := store.Delete(ctx, second.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get deleted err = %v", err)
	}
	if err := store.Delete(ctx, second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete missing err = %v", err)
	}

	listed, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != first.ID {
		t.Fatalf("List = %+v", listed)
	}
}

func TestMemoryStoreEmptyUpdate(t *testing.T) {
	store := NewMemoryStore()
	created, err := store.Create(context.Background(), CreateInput{Name: "One"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = store.Update(context.Background(), created.ID, UpdateInput{})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty update err = %v", err)
	}
}
