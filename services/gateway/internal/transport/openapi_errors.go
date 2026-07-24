package transport

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

// WriteOpenAPIParamError maps oapi-codegen binding failures to readable Russian errors
// instead of a single generic "Некорректные параметры запроса".
func WriteOpenAPIParamError(w http.ResponseWriter, _ *http.Request, err error) {
	if err == nil {
		apierror.Write(w, apierror.BadRequest("Некорректные параметры запроса"))
		return
	}

	var requiredHeader *api.RequiredHeaderError
	if errors.As(err, &requiredHeader) {
		switch requiredHeader.ParamName {
		case "Idempotency-Key":
			apierror.Write(w, apierror.BadRequest("Требуется заголовок Idempotency-Key"))
			return
		default:
			apierror.Write(w, apierror.BadRequest(fmt.Sprintf("Требуется заголовок %s", requiredHeader.ParamName)))
			return
		}
	}

	var requiredParam *api.RequiredParamError
	if errors.As(err, &requiredParam) {
		apierror.Write(w, apierror.BadRequest(fmt.Sprintf("Требуется параметр %s", requiredParam.ParamName)))
		return
	}

	var invalidFormat *api.InvalidParamFormatError
	if errors.As(err, &invalidFormat) {
		apierror.Write(w, apierror.BadRequest(fmt.Sprintf("Некорректный формат параметра %s", invalidFormat.ParamName)))
		return
	}

	var unmarshaling *api.UnmarshalingParamError
	if errors.As(err, &unmarshaling) {
		apierror.Write(w, apierror.BadRequest(fmt.Sprintf("Некорректное значение параметра %s", unmarshaling.ParamName)))
		return
	}

	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "Некорректные параметры запроса"
	}
	apierror.Write(w, apierror.BadRequest(message))
}
