package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/arenzana/correspondent/internal/store"
)

// Saved searches: a search kept in the sidebar like a folder.

func searchName(name, query string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = query
	}
	if utf8.RuneCountInString(name) > 80 {
		return "", errors.New("That name is too long.")
	}
	return name, nil
}

func (a *API) saveSearch(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Query string }
	if !decode(w, r, &in) {
		return
	}
	query := strings.TrimSpace(in.Query)
	if query == "" || utf8.RuneCountInString(query) > 200 {
		fail(w, http.StatusBadRequest, "Type what to search for (up to 200 characters).")
		return
	}
	name, err := searchName(in.Name, query)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s, err := a.Store.AddSearch(r.Context(), userID(r), name, query)
	if errors.Is(err, store.ErrTooManySearches) {
		fail(w, http.StatusConflict, fmt.Sprintf("You can keep up to %d searches. Remove one first.", store.MaxSearches))
		return
	} else if err != nil {
		a.internal(w, err)
		return
	}
	reply(w, map[string]any{"search": s})
}

func (a *API) renameSearch(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var in struct{ Name string }
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		fail(w, http.StatusBadRequest, "Give the search a name.")
		return
	}
	name, err := searchName(in.Name, "")
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	a.searchResult(w, a.Store.RenameSearch(r.Context(), userID(r), id, name))
}

func (a *API) deleteSearch(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	a.searchResult(w, a.Store.DeleteSearch(r.Context(), userID(r), id))
}

func (a *API) searchResult(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "No such saved search.")
	case err != nil:
		a.internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
