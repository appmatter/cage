package main

import (
	"path/filepath"
	"testing"
)

func TestChatStoreRename(t *testing.T) {
	dir := t.TempDir()
	s, err := newChatStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Rename(rec.ID, "  my topic  "); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "my topic" {
		t.Fatalf("title %q", got.Title)
	}
	list, _, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Title != "my topic" {
		t.Fatalf("list %#v", list)
	}
	// Blank title falls back to the default.
	if err := s.Rename(rec.ID, "   "); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New chat" {
		t.Fatalf("title %q", got.Title)
	}
	if err := s.Rename("chat-missing", "x"); err == nil {
		t.Fatal("expected error for missing chat")
	}
}

func TestChatStoreCreateAppendHydrate(t *testing.T) {
	dir := t.TempDir()
	s, err := newChatStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(rec.ID, map[string]any{"kind": "user", "text": "hello world"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(rec.ID, map[string]any{"kind": "text", "delta": "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(rec.ID, map[string]any{"kind": "text", "delta": " there"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(rec.ID, map[string]any{"kind": "done", "text": "req · completed"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "hello world" {
		t.Fatalf("title %q", got.Title)
	}
	turns, err := s.HydrateTurns(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 || turns[0]["role"] != "user" || turns[1]["text"] != "hi there" {
		t.Fatalf("turns %#v", turns)
	}
	list, active, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if active != rec.ID || len(list) != 1 {
		t.Fatalf("list %#v active %s", list, active)
	}
}
