// Package httpx contains transport helpers and middleware shared by TeamOS
// HTTP services. It does not contain domain-specific behavior.
package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"

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
	return decodeJSON(w, r, destination, maxBytes, false, false)
}

// DecodeJSONStrict is DecodeJSON, but rejects properties not represented by
// destination. Use it for operations where silently ignoring a property could
// incorrectly report that a requested state change was applied.
func DecodeJSONStrict(w http.ResponseWriter, r *http.Request, destination any, maxBytes int64) *apierror.Error {
	return decodeJSON(w, r, destination, maxBytes, false, true)
}

// DecodeJSONOptional is DecodeJSON, but an empty body is treated as "{}" so
// OpenAPI operations with optional request bodies can accept POST without a payload.
func DecodeJSONOptional(w http.ResponseWriter, r *http.Request, destination any, maxBytes int64) *apierror.Error {
	return decodeJSON(w, r, destination, maxBytes, true, false)
}

func decodeJSON(
	w http.ResponseWriter,
	r *http.Request,
	destination any,
	maxBytes int64,
	allowEmpty bool,
	rejectUnknownFields bool,
) *apierror.Error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	var strictBody bytes.Buffer
	var source io.Reader = r.Body
	if rejectUnknownFields {
		source = io.TeeReader(r.Body, &strictBody)
	}
	decoder := json.NewDecoder(source)
	if rejectUnknownFields {
		decoder.DisallowUnknownFields()
	}

	if err := decoder.Decode(destination); err != nil {
		var maxBytesErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxBytesErr):
			return apierror.BadRequest("Тело запроса превышает допустимый размер")
		case errors.Is(err, io.EOF):
			if allowEmpty {
				return nil
			}
			return apierror.BadRequest("Тело запроса не должно быть пустым")
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			return apierror.BadRequest("Тело запроса содержит неизвестное поле")
		default:
			return apierror.BadRequest("Некорректный JSON в теле запроса")
		}
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apierror.BadRequest("Тело запроса должно содержать один JSON-объект")
	}
	if rejectUnknownFields && hasNonExactJSONField(strictBody.Bytes(), destination) {
		return apierror.BadRequest("Тело запроса содержит неизвестное поле")
	}

	return nil
}

func hasNonExactJSONField(data []byte, destination any) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return false
	}

	destinationType := reflect.TypeOf(destination)
	for destinationType.Kind() == reflect.Pointer {
		destinationType = destinationType.Elem()
	}
	if destinationType.Kind() != reflect.Struct {
		return false
	}

	allowed := make(map[string]struct{}, destinationType.NumField())
	for index := 0; index < destinationType.NumField(); index++ {
		field := destinationType.Field(index)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		switch name {
		case "-":
			continue
		case "":
			name = field.Name
		}
		allowed[name] = struct{}{}
	}
	for name := range object {
		if _, ok := allowed[name]; !ok {
			return true
		}
	}
	return false
}
