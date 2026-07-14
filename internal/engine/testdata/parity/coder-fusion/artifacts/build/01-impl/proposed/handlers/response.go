package handlers

import (
	"encoding/json"
	"net/http"
)

// envelopeError defines the JSON structure used by writeError.
type envelopeError struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}

// writeJSON serialises v as JSON, sets the appropriate Content-Type header,
// writes the supplied status code and sends the response.
func writeJSON(w http.ResponseWriter, v interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError formats err into a JSON envelope and writes it with the supplied
// HTTP status code.
func writeError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelopeError{
		Error: err.Error(),
		Code:  status,
	})
}