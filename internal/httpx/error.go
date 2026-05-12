package httpx

import (
	"encoding/json"
	"log"
	"net/http"
)

// WriteJSON serialises v to the response with the given status code.
// On marshal failure it logs server-side and falls back to an empty body
// so we never leak a partial JSON object to the client.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		log.Printf("httpx: marshal failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if _, werr := w.Write([]byte(`{"error":"something went wrong","code":"INTERNAL"}`)); werr != nil {
			log.Printf("httpx: write fallback failed: %v", werr)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, werr := w.Write(body); werr != nil {
		log.Printf("httpx: write failed: %v", werr)
	}
}

// RespondError writes a uniform JSON error.
// Shape: {"error": "<safe message>", "code": "<MACHINE_CODE>"}.
// Never include internal error text — log it server-side via InternalError.
func RespondError(w http.ResponseWriter, status int, code, msg string) {
	WriteJSON(w, status, map[string]string{
		"error": msg,
		"code":  code,
	})
}

// InternalError logs the underlying error (with the request ID for
// correlation) and returns a generic 500 to the client.
func InternalError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("req=%s internal_error=%q", RequestID(r.Context()), err.Error())
	RespondError(w, http.StatusInternalServerError, "INTERNAL", "something went wrong")
}
