// Package httpx contains transport helpers and middleware shared by TeamOS
// HTTP services. It does not contain domain-specific behavior.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/sk1fy/team-os-backend/pkg/apierror"
)

const DefaultMaxBodyBytes int64 = 1 << 20 // 1 MiB, including TipTap JSON.

// WriteJSON writes a JSON response and returns an encoding/write error to the
// caller for logging. Headers are not committed until encoding succeeds.
func WriteJSON(w http.ResponseWriter, status int, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, err = w.Write(append(data, '\n'))
	return err
}

// DecodeJSON decodes exactly one JSON value and limits the request size.
// Unknown fields are intentionally ignored so additive clients can be deployed
// before every service instance has rolled to the new contract version.
func DecodeJSON(w http.ResponseWriter, r *http.Request, destination any, maxBytes int64) *apierror.Error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(destination); err != nil {
		var maxBytesErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxBytesErr):
			return apierror.BadRequest("Тело запроса превышает допустимый размер")
		case errors.Is(err, io.EOF):
			return apierror.BadRequest("Тело запроса не должно быть пустым")
		default:
			return apierror.BadRequest("Некорректный JSON в теле запроса")
		}
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apierror.BadRequest("Тело запроса должно содержать один JSON-объект")
	}

	return nil
}
