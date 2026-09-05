package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/platform/apperr"
)

// MaxBodyBytes caps request bodies; without a limit a single request could
// exhaust memory.
const MaxBodyBytes = 1 << 20 // 1 MiB

// DecodeJSON reads a JSON body into target, reporting malformed input as a
// bad request rather than letting a decode error surface as a 500.
//
// Unknown fields are accepted, so a client sending an extra property keeps
// working.
func DecodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)

	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			return apperr.BadRequest("request body must not exceed %d bytes", maxBytes.Limit)
		}
		if errors.Is(err, io.EOF) {
			return apperr.BadRequest("request body is required")
		}
		return apperr.BadRequest("request body is not valid JSON")
	}

	return nil
}
