package typesafe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAsk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req struct {
			Model     string
			Questions map[string]struct{ Type, Instructions string }
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != Model || req.Questions["q"].Type != "noul" {
			t.Errorf("request %+v", req)
		}
		w.Write([]byte(`{"answers":{"q":{"type":"noul","noul":0.93}},"usage":{"input_tokens":10}}`))
	}))
	defer srv.Close()
	c := &Client{Key: "good", URL: srv.URL}
	ps, err := c.Ask(context.Background(), "state", map[string]Noul{"q": {Instructions: "Is it?"}})
	if err != nil || ps["q"] != 0.93 {
		t.Fatalf("%v %v", ps, err)
	}
	c.Key = "bad"
	if _, err := c.Ask(context.Background(), "state", map[string]Noul{"q": {Instructions: "Is it?"}}); err == nil {
		t.Fatal("a rejected key isn't an error")
	}
}
