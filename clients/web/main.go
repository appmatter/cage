// Command web is a minimal HTMX UI that speaks to the Cage supervisor.
// The browser never opens the supervisor socket.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

//go:embed index.html
var indexFS embed.FS

func main() {
	configPath := flag.String("config", "config.json", "web UI config (listen, supervisor, Cage client context)")
	listen := flag.String("listen", "", "override config listen")
	supervisor := flag.String("supervisor", "", "override Cage supervisor URL")
	token := flag.String("token", "", "override config bearer token")
	cage := flag.String("cage", "", "override Cage client context URL")
	flag.Parse()

	cfg, err := loadWebConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "web: %v\n", err)
		os.Exit(1)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *supervisor != "" {
		cfg.Supervisor = *supervisor
	}
	if *token != "" {
		cfg.Token = *token
	}
	if *cage != "" {
		cfg.Cage = *cage
	}
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(filepath.Dir(*configPath), ".agent-web")
	} else if !filepath.IsAbs(cfg.DataDir) {
		cfg.DataDir = filepath.Join(filepath.Dir(*configPath), cfg.DataDir)
	}

	mentions := newCageMentions(cfg.Cage, cfg.Token)
	store, err := newChatStore(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "web: data_dir %q: %v\n", cfg.DataDir, err)
		os.Exit(1)
	}

	c := newClient(cfg.Supervisor, cfg.Token, store, mentions)
	go c.loop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, err := indexFS.ReadFile("index.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	})
	mux.HandleFunc("GET /mentions", mentions.handleSuggest)
	mux.HandleFunc("GET /chats", c.handleListChats)
	mux.HandleFunc("POST /chats", c.handleCreateChat)
	mux.HandleFunc("GET /chats/{id}", c.handleGetChat)
	mux.HandleFunc("POST /chats/{id}/select", c.handleSelectChat)
	mux.HandleFunc("POST /chats/{id}/rename", c.handleRenameChat)
	mux.HandleFunc("POST /prompt", c.handlePrompt)
	mux.HandleFunc("POST /interrupt", c.handleControl("interrupt"))
	mux.HandleFunc("POST /restart", c.handleRestart)
	mux.HandleFunc("POST /stop", c.handleStop)
	mux.HandleFunc("GET /events", c.handleSSE)

	log.Printf("ui %s → supervisor %s, Cage %s (data %s)", cfg.Listen, cfg.Supervisor, cfg.Cage, cfg.DataDir)
	if err := http.ListenAndServe(cfg.Listen, mux); err != nil {
		fmt.Fprintf(os.Stderr, "web: %v\n", err)
		os.Exit(1)
	}
}

type client struct {
	url      string
	token    string
	store    *chatStore
	mentions *cageMentions

	seq  atomic.Uint64
	mu   sync.Mutex
	conn *websocket.Conn

	activeChat string
	// chatID -> live supervisor session ID
	live map[string]string
	// sessionId -> chatID
	bySession map[string]string
	// requestId -> waiter for control/hydrate handshakes
	waiters map[string]chan wireEvent

	subMu sync.Mutex
	subs  map[chan string]struct{}
}

type wireEvent struct {
	Event     string `json:"event"`
	RequestID string `json:"requestId"`
	SessionID string `json:"sessionId"`
	Payload   struct {
		Reason    string `json:"reason"`
		SessionID string `json:"sessionId"`
		Code      string `json:"code"`
		Message   string `json:"message"`
	} `json:"payload"`
	Raw []byte
}

// Large tool events (e.g. ls -R) must not trip the WebSocket read limit.
const maxFrameBytes = 1 << 20

func newClient(url, token string, store *chatStore, mentions *cageMentions) *client {
	return &client{
		url:       url,
		token:     token,
		store:     store,
		mentions:  mentions,
		live:      make(map[string]string),
		bySession: make(map[string]string),
		waiters:   make(map[string]chan wireEvent),
		subs:      make(map[chan string]struct{}),
	}
}

func (c *client) loop() {
	for {
		if err := c.connect(); err != nil {
			c.publish(map[string]any{"kind": "error", "text": "supervisor: " + err.Error()})
			time.Sleep(time.Second)
			continue
		}
		c.publish(map[string]any{"kind": "status", "text": "connected"})
		c.readLoop()
		c.clearLive()
		c.publish(map[string]any{"kind": "error", "text": "supervisor disconnected"})
		time.Sleep(time.Second)
	}
}

func (c *client) connect() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opts := &websocket.DialOptions{}
	if c.token != "" {
		opts.HTTPHeader = http.Header{"Authorization": []string{"Bearer " + c.token}}
	}
	conn, _, err := websocket.Dial(ctx, c.url, opts)
	if err != nil {
		return err
	}
	conn.SetReadLimit(maxFrameBytes)
	c.mu.Lock()
	if c.conn != nil {
		c.conn.CloseNow()
	}
	c.conn = conn
	c.mu.Unlock()
	return nil
}

func (c *client) clearLive() {
	c.mu.Lock()
	c.live = make(map[string]string)
	c.bySession = make(map[string]string)
	for id, ch := range c.waiters {
		close(ch)
		delete(c.waiters, id)
	}
	c.mu.Unlock()
}

func (c *client) readLoop() {
	ctx := context.Background()
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		typ, data, err := conn.Read(ctx)
		if err != nil {
			c.mu.Lock()
			if c.conn == conn {
				c.conn = nil
			}
			c.mu.Unlock()
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var ev wireEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			c.publish(map[string]any{"kind": "error", "text": string(data)})
			continue
		}
		ev.Raw = data

		c.mu.Lock()
		waiter := c.waiters[ev.RequestID]
		if waiter != nil {
			delete(c.waiters, ev.RequestID)
		}
		chatID := c.bySession[ev.SessionID]
		active := c.activeChat
		c.mu.Unlock()

		if waiter != nil {
			select {
			case waiter <- ev:
			default:
			}
			if ev.Event == "error" || ev.Event == "done" {
				continue
			}
		}

		payload := formatEvent(data)
		if chatID != "" {
			payload["chatId"] = chatID
			payload["sessionId"] = ev.SessionID
			if shouldPersist(payload) {
				_ = c.store.Append(chatID, cloneEntry(payload))
			}
		} else if ev.Event == "done" || ev.Event == "error" {
			continue
		}
		if chatID == "" || chatID == active || payload["kind"] == "error" {
			c.publish(payload)
		} else if payload["kind"] == "done" || payload["kind"] == "text" || payload["kind"] == "tool" {
			c.publish(map[string]any{
				"kind":   "chat_activity",
				"chatId": chatID,
				"text":   "running",
			})
		}
	}
}

func shouldPersist(payload map[string]any) bool {
	kind, _ := payload["kind"].(string)
	switch kind {
	case "user", "text", "thinking", "tool", "error", "done", "status":
		return true
	default:
		// "run" is ephemeral in-flight signal — do not store.
		return false
	}
}

func cloneEntry(payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		if k == "chatId" || k == "sessionId" {
			continue
		}
		out[k] = v
	}
	return out
}

func (c *client) handleListChats(w http.ResponseWriter, r *http.Request) {
	chats, active, err := c.store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	c.mu.Lock()
	live := make(map[string]bool, len(c.live))
	for id := range c.live {
		live[id] = true
	}
	if c.activeChat != "" {
		active = c.activeChat
	}
	c.mu.Unlock()
	type row struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		UpdatedAt time.Time `json:"updatedAt"`
		Live      bool      `json:"live"`
	}
	out := make([]row, 0, len(chats))
	for _, ch := range chats {
		out = append(out, row{ID: ch.ID, Title: ch.Title, UpdatedAt: ch.UpdatedAt, Live: live[ch.ID]})
	}
	writeJSON(w, map[string]any{"chats": out, "activeId": active})
}

func (c *client) handleCreateChat(w http.ResponseWriter, r *http.Request) {
	rec, err := c.store.Create("")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := c.ensureLive(rec.ID, true); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	c.mu.Lock()
	c.activeChat = rec.ID
	c.mu.Unlock()
	_ = c.store.SetActive(rec.ID)
	writeJSON(w, rec)
}

func (c *client) handleGetChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := c.store.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	c.mu.Lock()
	sid := c.live[id]
	c.mu.Unlock()
	writeJSON(w, map[string]any{
		"id": rec.ID, "title": rec.Title, "updatedAt": rec.UpdatedAt,
		"entries": rec.Entries, "live": sid != "", "sessionId": sid,
	})
}

func (c *client) handleRenameChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Title) == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if err := c.store.Rename(id, body.Title); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	c.publish(map[string]any{"kind": "chat_renamed", "chatId": id, "title": strings.TrimSpace(body.Title)})
	w.WriteHeader(http.StatusNoContent)
}

func (c *client) handleSelectChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := c.store.Get(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := c.ensureLive(id, false); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	c.mu.Lock()
	c.activeChat = id
	c.mu.Unlock()
	_ = c.store.SetActive(id)
	w.WriteHeader(http.StatusNoContent)
}

func (c *client) ensureLive(chatID string, fresh bool) error {
	c.mu.Lock()
	if sid := c.live[chatID]; sid != "" {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	sid, err := c.openAndStart()
	if err != nil {
		return err
	}
	c.mu.Lock()
	if owner, taken := c.bySession[sid]; taken && owner != chatID {
		c.mu.Unlock()
		return fmt.Errorf("session id %s already owned by another chat", sid)
	}
	// Bind before hydrate so any events route to this chat.
	c.live[chatID] = sid
	c.bySession[sid] = chatID
	c.mu.Unlock()

	if !fresh {
		if err := c.rehydrate(chatID, sid); err != nil {
			return err
		}
	}
	return nil
}

// rehydrate restores prior turns from disk into the live harness session.
// Restart / Bind clears adapter history; without this the UI scrollback and
// model context diverge while the chat stays marked live.
func (c *client) rehydrate(chatID, sid string) error {
	turns, err := c.store.HydrateTurns(chatID)
	if err != nil {
		return err
	}
	if len(turns) == 0 {
		return nil
	}
	return c.hydrate(sid, turns)
}

func (c *client) openAndStart() (string, error) {
	// Client-chosen, process-unique session ID so the supervisor cannot hand back
	// an id already bound to another live chat (which would make bySession a
	// lossy map and misroute events for every chat that isn't the last one
	// to claim the recycled id). The id comes from the same process-wide
	// sequence as request ids, so it also stays unique across reconnects
	// (supervisor session IDs are unique for the server process lifetime).
	sid := c.nextID("sess")
	openID := c.nextID("ctl")
	done, err := c.expect(openID)
	if err != nil {
		return "", err
	}
	defer c.cancelExpect(openID)
	if err := c.write(map[string]any{
		"v": 1, "id": openID, "type": "control", "action": "open_session", "sessionId": sid,
	}); err != nil {
		return "", err
	}
	ev, err := waitWire(done, 10*time.Second)
	if err != nil {
		return "", err
	}
	if ev.Event == "error" {
		return "", fmt.Errorf("%s: %s", ev.Payload.Code, ev.Payload.Message)
	}
	realSid := ev.SessionID
	if realSid == "" {
		realSid = ev.Payload.SessionID
	}
	if realSid == "" {
		return "", fmt.Errorf("open_session missing sessionId")
	}
	sid = realSid

	startID := c.nextID("ctl")
	done, err = c.expect(startID)
	if err != nil {
		return "", err
	}
	if err := c.write(map[string]any{
		"v": 1, "id": startID, "type": "control", "sessionId": sid, "action": "start",
	}); err != nil {
		c.cancelExpect(startID)
		return "", err
	}
	ev, err = waitWire(done, 30*time.Second)
	if err != nil {
		return "", err
	}
	if ev.Event == "error" {
		return "", fmt.Errorf("%s: %s", ev.Payload.Code, ev.Payload.Message)
	}
	return sid, nil
}

func (c *client) hydrate(sid string, turns []map[string]string) error {
	id := c.nextID("hyd")
	done, err := c.expect(id)
	if err != nil {
		return err
	}
	if err := c.write(map[string]any{
		"v": 1, "id": id, "type": "hydrate", "sessionId": sid, "turns": turns,
	}); err != nil {
		c.cancelExpect(id)
		return err
	}
	ev, err := waitWire(done, 30*time.Second)
	if err != nil {
		return err
	}
	if ev.Event == "error" {
		return fmt.Errorf("%s: %s", ev.Payload.Code, ev.Payload.Message)
	}
	return nil
}

func (c *client) expect(requestID string) (chan wireEvent, error) {
	ch := make(chan wireEvent, 2)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil, fmt.Errorf("not connected to Cage supervisor")
	}
	c.waiters[requestID] = ch
	return ch, nil
}

func (c *client) cancelExpect(requestID string) {
	c.mu.Lock()
	ch := c.waiters[requestID]
	delete(c.waiters, requestID)
	c.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func waitWire(ch chan wireEvent, d time.Duration) (wireEvent, error) {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return wireEvent{}, fmt.Errorf("waiter closed")
			}
			if ev.Event == "error" || ev.Event == "done" {
				return ev, nil
			}
		case <-t.C:
			return wireEvent{}, fmt.Errorf("timeout waiting for Cage supervisor")
		}
	}
}

func (c *client) activeSession() (chatID, sid string, err error) {
	c.mu.Lock()
	chatID = c.activeChat
	sid = c.live[chatID]
	c.mu.Unlock()
	if chatID == "" {
		return "", "", fmt.Errorf("no active chat")
	}
	if sid == "" {
		if err := c.ensureLive(chatID, false); err != nil {
			return "", "", err
		}
		c.mu.Lock()
		sid = c.live[chatID]
		c.mu.Unlock()
	}
	if sid == "" {
		return "", "", fmt.Errorf("no live session")
	}
	return chatID, sid, nil
}

func (c *client) handlePrompt(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	prompt := strings.TrimSpace(r.FormValue("prompt"))
	if prompt == "" {
		http.Error(w, "prompt required", http.StatusBadRequest)
		return
	}
	chatID, sid, err := c.activeSession()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	userEntry := map[string]any{"kind": "user", "text": prompt}
	_ = c.store.Append(chatID, userEntry)

	msg := map[string]any{
		"v": 1, "id": c.nextID("req"), "type": "prompt", "sessionId": sid, "prompt": prompt,
	}
	if raw := r.FormValue("mentions"); raw != "" {
		mentions, err := c.mentions.resolve(raw)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(mentions) > 0 {
			msg["context"] = map[string]any{"mentions": mentions}
		}
	}
	if err := c.write(msg); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *client) handleControl(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, sid, err := c.activeSession()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if err := c.write(map[string]any{
			"v": 1, "id": c.nextID("ctl"), "type": "control", "sessionId": sid, "action": action,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleRestart respawns the harness then rehydrates model context from the
// chat store. Without the second step, Restart clears Pi history while the UI
// keeps scrollback and ensureLive skips hydrate because the chat is still live.
func (c *client) handleRestart(w http.ResponseWriter, r *http.Request) {
	chatID, sid, err := c.activeSession()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	id := c.nextID("ctl")
	done, err := c.expect(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := c.write(map[string]any{
		"v": 1, "id": id, "type": "control", "sessionId": sid, "action": "restart",
	}); err != nil {
		c.cancelExpect(id)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// Stop+start can take longer than a plain start.
	ev, err := waitWire(done, 60*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if ev.Event == "error" {
		http.Error(w, fmt.Sprintf("%s: %s", ev.Payload.Code, ev.Payload.Message), http.StatusBadGateway)
		return
	}
	if err := c.rehydrate(chatID, sid); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleStop stops the harness and drops the live binding so the next prompt
// opens a fresh session instead of failing with harness_not_running.
func (c *client) handleStop(w http.ResponseWriter, r *http.Request) {
	chatID, sid, err := c.activeSession()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	id := c.nextID("ctl")
	done, err := c.expect(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := c.write(map[string]any{
		"v": 1, "id": id, "type": "control", "sessionId": sid, "action": "stop",
	}); err != nil {
		c.cancelExpect(id)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	ev, err := waitWire(done, 30*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if ev.Event == "error" {
		http.Error(w, fmt.Sprintf("%s: %s", ev.Payload.Code, ev.Payload.Message), http.StatusBadGateway)
		return
	}
	c.dropLive(chatID, sid)
	closeID := c.nextID("ctl")
	// Best-effort: free the supervisor session slot. Ignore failures — local live
	// state is already cleared so the next prompt will open a new session.
	_ = c.write(map[string]any{
		"v": 1, "id": closeID, "type": "control", "sessionId": sid, "action": "close_session",
	})
	w.WriteHeader(http.StatusNoContent)
}

func (c *client) dropLive(chatID, sid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.live[chatID] == sid {
		delete(c.live, chatID)
	}
	if c.bySession[sid] == chatID {
		delete(c.bySession, sid)
	}
}

func (c *client) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := c.subscribe()
	defer c.unsubscribe(ch)
	for {
		select {
		case <-r.Context().Done():
			return
		case line := <-ch:
			fmt.Fprintf(w, "event: event\ndata: %s\n\n", line)
			flusher.Flush()
		}
	}
}

func (c *client) write(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("not connected to Cage supervisor")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, data)
}

func (c *client) nextID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, c.seq.Add(1))
}

func (c *client) subscribe() chan string {
	ch := make(chan string, 64)
	c.subMu.Lock()
	c.subs[ch] = struct{}{}
	c.subMu.Unlock()
	return ch
}

func (c *client) unsubscribe(ch chan string) {
	c.subMu.Lock()
	delete(c.subs, ch)
	c.subMu.Unlock()
}

func (c *client) publish(payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	line := string(data)
	c.subMu.Lock()
	defer c.subMu.Unlock()
	for ch := range c.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func formatEvent(data []byte) map[string]any {
	var ev struct {
		Event     string          `json:"event"`
		RequestID string          `json:"requestId"`
		SessionID string          `json:"sessionId"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return map[string]any{"kind": "error", "text": string(data)}
	}
	var payload map[string]any
	_ = json.Unmarshal(ev.Payload, &payload)
	switch ev.Event {
	case "text":
		return map[string]any{"kind": "text", "delta": str(payload["delta"]), "requestId": ev.RequestID}
	case "thinking":
		return map[string]any{"kind": "thinking", "delta": str(payload["delta"]), "requestId": ev.RequestID}
	case "tool":
		out := map[string]any{
			"kind":      "tool",
			"phase":     str(payload["phase"]),
			"name":      str(payload["name"]),
			"callId":    str(payload["callId"]),
			"requestId": ev.RequestID,
		}
		if v, ok := payload["args"]; ok {
			out["args"] = v
		}
		if v, ok := payload["result"]; ok {
			out["result"] = v
		}
		if v, ok := payload["ok"]; ok {
			out["ok"] = v
		}
		return out
	case "status":
		phase := str(payload["phase"])
		if phase == "running" {
			// Prompt accepted and in flight (protocol status event).
			return map[string]any{"kind": "run", "phase": phase, "requestId": ev.RequestID}
		}
		return map[string]any{"kind": "status", "text": phase, "requestId": ev.RequestID}
	case "error":
		return map[string]any{
			"kind":      "error",
			"text":      str(payload["code"]) + ": " + str(payload["message"]),
			"requestId": ev.RequestID,
		}
	case "done":
		return map[string]any{
			"kind":      "done",
			"text":      ev.RequestID + " · " + str(payload["reason"]),
			"requestId": ev.RequestID,
			"reason":    str(payload["reason"]),
		}
	default:
		return map[string]any{"kind": "status", "text": ev.Event}
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
