package httpx

import (
	"encoding/json"
	"net/http"
)

// JSON writes {"data": data} with the given status.
func JSON[T any](w http.ResponseWriter, status int, data T) error {
	return writeJSON(w, status, struct {
		Data T `json:"data"`
	}{data})
}

// List writes {"data": [...], "nextCursor": "..." | null}. A nil items slice
// is sent as [] and an empty cursor as null.
func List[T any](w http.ResponseWriter, items []T, nextCursor string) error {
	if items == nil {
		items = []T{}
	}
	var cursor *string
	if nextCursor != "" {
		cursor = &nextCursor
	}
	return writeJSON(w, http.StatusOK, struct {
		Data       []T     `json:"data"`
		NextCursor *string `json:"nextCursor"`
	}{items, cursor})
}

func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, body any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(body)
}
