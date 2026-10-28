// Package typesafe asks TypeSafe's Jev model yes-or-no questions (its Noul
// judgments) about a piece of state. The server uses it, when an admin
// gives it a key, to tell an article's own blocks from the website material
// around them where its rules can't decide. Without a key nothing calls it.
package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Endpoint is TypeSafe's System One API.
const Endpoint = "https://api.typesafe.ai/v1/systemone"

// Model is the Jev version asked.
const Model = "jev-latest"

// Client asks Jev questions with one API key.
type Client struct {
	Key  string
	HTTP *http.Client // nil: a client with a 20-second timeout
	URL  string       // "" means Endpoint; set by tests
}

// Noul is a yes-or-no question: Instructions is the statement judged,
// True and False say what each answer looks like.
type Noul struct {
	Instructions string
	True, False  string
}

type question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

// Ask judges each question against state, returning the probability that
// each is true (0 to 1), by the questions' keys.
func (c *Client) Ask(ctx context.Context, state any, questions map[string]Noul) (map[string]float64, error) {
	qs := make(map[string]question, len(questions))
	for k, q := range questions {
		qs[k] = question{Type: "noul", Instructions: q.Instructions, Criteria: map[string]string{"true": q.True, "false": q.False}}
	}
	body, err := json.Marshal(map[string]any{"model": Model, "state": state, "questions": qs})
	if err != nil {
		return nil, err
	}
	url := c.URL
	if url == "" {
		url = Endpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, errors.New("TypeSafe rejected the API key")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("TypeSafe answered %d", resp.StatusCode)
	}
	var out struct {
		Answers map[string]struct {
			Noul float64 `json:"noul"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("TypeSafe's reply wasn't understood: %w", err)
	}
	ps := make(map[string]float64, len(out.Answers))
	for k := range questions {
		a, ok := out.Answers[k]
		if !ok {
			return nil, fmt.Errorf("TypeSafe's reply has no answer for %q", k)
		}
		ps[k] = a.Noul
	}
	return ps, nil
}
