package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const mentionLimit = 20

// cageMentions calls Cage's configured fs.plugins.mention capability.
type cageMentions struct {
	url   string
	token string
	http  *http.Client
}

type cageMention struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Content string `json:"content,omitempty"`
}

func newCageMentions(url, token string) *cageMentions {
	return &cageMentions{url: strings.TrimRight(url, "/"), token: token, http: http.DefaultClient}
}

func (c *cageMentions) handleSuggest(w http.ResponseWriter, r *http.Request) {
	mentions, err := c.call("suggest", map[string]any{
		"query": strings.TrimPrefix(strings.TrimSpace(r.URL.Query().Get("q")), "@"),
		"limit": mentionLimit,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"paths": mentions})
}

func (c *cageMentions) resolve(raw string) ([]map[string]any, error) {
	var selected []cageMention
	if err := json.Unmarshal([]byte(raw), &selected); err != nil {
		return nil, fmt.Errorf("mentions: %w", err)
	}
	mentions, err := c.call("resolve", selected)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(mentions))
	for _, mention := range mentions {
		entry := map[string]any{"path": mention.Path, "kind": mention.Kind}
		if mention.Content != "" {
			entry["text"] = mention.Content
		}
		out = append(out, entry)
	}
	return out, nil
}

func (c *cageMentions) call(operation string, payload any) ([]cageMention, error) {
	if c.url == "" {
		return nil, fmt.Errorf("Cage client context URL is required")
	}
	body, err := json.Marshal(map[string]any{"operation": operation, "payload": payload})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.url+"/v1/context/fs/plugins/mention/call", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Cage mention plugin: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("Cage mention plugin: %s: %s", res.Status, strings.TrimSpace(string(data)))
	}
	var response struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	var mentions []cageMention
	if err := json.Unmarshal(response.Payload, &mentions); err != nil {
		return nil, err
	}
	return mentions, nil
}
