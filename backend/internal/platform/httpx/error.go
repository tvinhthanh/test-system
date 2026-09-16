package httpx

import (
	"encoding/json"
	"net/http"
)

// ErrorBody is the §7.1 envelope: {"error": {"code", "message", "details"}}.
// Code is machine-readable and stable; message is for humans and must not
// carry a stack trace, SQL or a path (§11.5).
type ErrorBody struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

// WriteError writes the envelope with the given status. Details may be nil;
// it is rendered as {} so clients can always index it.
func WriteError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	var body ErrorBody
	body.Error.Code = code
	body.Error.Message = message
	body.Error.Details = details
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
