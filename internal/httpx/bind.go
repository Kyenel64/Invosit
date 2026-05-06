package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New(validator.WithRequiredStructEnabled())

// Bind decodes a JSON request body into v and validates it against
// `validate:"..."` struct tags. Unknown fields are rejected.
//
// Returns an error suitable for treating as a 400. Caller is responsible
// for mapping it to a client-safe message — never echo err.Error() back.
func Bind(r *http.Request, v any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("empty body")
		}
		return err
	}
	return validate.Struct(v)
}
