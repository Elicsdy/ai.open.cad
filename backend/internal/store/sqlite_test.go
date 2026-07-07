package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteProjectCRUD(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	created, err := db.CreateProject(ctx, ProjectInput{
		Title:    "Box",
		Prompt:   "make a box",
		Code:     "Box(1, 2, 3, true);",
		Language: "cascade-js",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("expected id")
	}

	list, err := db.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 project, got %d", len(list))
	}

	updated, err := db.UpdateProject(ctx, created.ID, ProjectInput{
		Title:    "Updated",
		Prompt:   "updated prompt",
		Code:     "Box(4, 5, 6, true);",
		Language: "cascade-js",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Updated" {
		t.Fatalf("unexpected title: %s", updated.Title)
	}

	if err := db.DeleteProject(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetProject(ctx, created.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSQLiteProjectOwnerIsolation(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	alice, err := db.CreateProjectForOwner(ctx, "alice", ProjectInput{
		Title:    "Alice Box",
		Prompt:   "make alice box",
		Code:     "Box(1, 1, 1, true);",
		Language: "cascade-js",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateProjectForOwner(ctx, "bob", ProjectInput{
		Title:    "Bob Box",
		Prompt:   "make bob box",
		Code:     "Box(2, 2, 2, true);",
		Language: "cascade-js",
	}); err != nil {
		t.Fatal(err)
	}

	aliceList, err := db.ListProjectsForOwner(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceList) != 1 || aliceList[0].Title != "Alice Box" {
		t.Fatalf("unexpected alice list: %+v", aliceList)
	}
	if _, err := db.GetProjectForOwner(ctx, "bob", alice.ID); err != ErrNotFound {
		t.Fatalf("expected owner isolation, got %v", err)
	}
}
