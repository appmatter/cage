package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// chatStore is durable chat history on disk, separate from Cage sessions.
type chatStore struct {
	dir string
	mu  sync.Mutex
}

type chatSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type chatRecord struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Entries   []map[string]any `json:"entries"`
}

type storeIndex struct {
	ActiveID string        `json:"activeId"`
	Chats    []chatSummary `json:"chats"`
}

func newChatStore(dir string) (*chatStore, error) {
	if err := os.MkdirAll(filepath.Join(dir, "chats"), 0o755); err != nil {
		return nil, err
	}
	return &chatStore{dir: dir}, nil
}

func (s *chatStore) indexPath() string { return filepath.Join(s.dir, "index.json") }
func (s *chatStore) chatPath(id string) string {
	return filepath.Join(s.dir, "chats", id+".json")
}

func (s *chatStore) loadIndex() (storeIndex, error) {
	data, err := os.ReadFile(s.indexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return storeIndex{}, nil
		}
		return storeIndex{}, err
	}
	var idx storeIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return storeIndex{}, err
	}
	return idx, nil
}

func (s *chatStore) saveIndex(idx storeIndex) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.indexPath(), data, 0o644)
}

func (s *chatStore) List() ([]chatSummary, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.loadIndex()
	if err != nil {
		return nil, "", err
	}
	sort.SliceStable(idx.Chats, func(i, j int) bool {
		return idx.Chats[i].UpdatedAt.After(idx.Chats[j].UpdatedAt)
	})
	return idx.Chats, idx.ActiveID, nil
}

func (s *chatStore) Get(id string) (*chatRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readChat(id)
}

func (s *chatStore) readChat(id string) (*chatRecord, error) {
	data, err := os.ReadFile(s.chatPath(id))
	if err != nil {
		return nil, err
	}
	var rec chatRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *chatStore) writeChat(rec *chatRecord) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.chatPath(rec.ID), data, 0o644)
}

func (s *chatStore) Create(title string) (*chatRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := newChatID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if title == "" {
		title = "New chat"
	}
	rec := &chatRecord{ID: id, Title: title, UpdatedAt: now, Entries: []map[string]any{}}
	if err := s.writeChat(rec); err != nil {
		return nil, err
	}
	idx, err := s.loadIndex()
	if err != nil {
		return nil, err
	}
	idx.Chats = append(idx.Chats, chatSummary{ID: id, Title: title, UpdatedAt: now})
	idx.ActiveID = id
	if err := s.saveIndex(idx); err != nil {
		return nil, err
	}
	return rec, nil
}

func (s *chatStore) Rename(id, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.readChat(id)
	if err != nil {
		return err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "New chat"
	}
	rec.Title = title
	rec.UpdatedAt = time.Now().UTC()
	if err := s.writeChat(rec); err != nil {
		return err
	}
	idx, err := s.loadIndex()
	if err != nil {
		return err
	}
	for i := range idx.Chats {
		if idx.Chats[i].ID == id {
			idx.Chats[i].Title = rec.Title
			idx.Chats[i].UpdatedAt = rec.UpdatedAt
			break
		}
	}
	return s.saveIndex(idx)
}

func (s *chatStore) SetActive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.readChat(id); err != nil {
		return err
	}
	idx, err := s.loadIndex()
	if err != nil {
		return err
	}
	idx.ActiveID = id
	return s.saveIndex(idx)
}

func (s *chatStore) Append(id string, entry map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.readChat(id)
	if err != nil {
		return err
	}
	rec.Entries = append(rec.Entries, entry)
	rec.UpdatedAt = time.Now().UTC()
	if rec.Title == "New chat" {
		if kind, _ := entry["kind"].(string); kind == "user" {
			if text, _ := entry["text"].(string); text != "" {
				rec.Title = truncateTitle(text)
			}
		}
	}
	if err := s.writeChat(rec); err != nil {
		return err
	}
	idx, err := s.loadIndex()
	if err != nil {
		return err
	}
	for i := range idx.Chats {
		if idx.Chats[i].ID == id {
			idx.Chats[i].Title = rec.Title
			idx.Chats[i].UpdatedAt = rec.UpdatedAt
			break
		}
	}
	return s.saveIndex(idx)
}

func (s *chatStore) ClearEntries(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.readChat(id)
	if err != nil {
		return err
	}
	rec.Entries = []map[string]any{}
	rec.UpdatedAt = time.Now().UTC()
	if err := s.writeChat(rec); err != nil {
		return err
	}
	idx, err := s.loadIndex()
	if err != nil {
		return err
	}
	for i := range idx.Chats {
		if idx.Chats[i].ID == id {
			idx.Chats[i].UpdatedAt = rec.UpdatedAt
			break
		}
	}
	return s.saveIndex(idx)
}

func (s *chatStore) HydrateTurns(id string) ([]map[string]string, error) {
	rec, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	var turns []map[string]string
	var assistant strings.Builder
	flushAssistant := func() {
		if assistant.Len() == 0 {
			return
		}
		turns = append(turns, map[string]string{"role": "assistant", "text": assistant.String()})
		assistant.Reset()
	}
	for _, e := range rec.Entries {
		kind, _ := e["kind"].(string)
		switch kind {
		case "user":
			flushAssistant()
			text, _ := e["text"].(string)
			turns = append(turns, map[string]string{"role": "user", "text": text})
		case "text":
			delta, _ := e["delta"].(string)
			assistant.WriteString(delta)
		case "done", "error":
			flushAssistant()
		}
	}
	flushAssistant()
	return turns, nil
}

func newChatID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "chat-" + hex.EncodeToString(b[:]), nil
}

func truncateTitle(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 48 {
		return s
	}
	return s[:45] + "…"
}
