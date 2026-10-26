package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteData writes {"data": payload} — the shape the existing ZETRC
// frontend's extractRows()/findRecord() helpers already know how to parse.
func WriteData(w http.ResponseWriter, status int, payload any) {
	writeJSON(w, status, map[string]any{"data": payload})
}

// WriteError writes {"error": message}, which the frontend's pickMessage()
// helper surfaces directly to the user.
func WriteError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// DecodeJSON reads and JSON-decodes the request body into dst.
func DecodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	return dec.Decode(dst)
}
