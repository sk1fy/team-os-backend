package transport

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

// Ключи, придуманные gateway вместо клиента, живут в отдельном пространстве имён.
// Переиграть такую reservation невозможно: ключ не знает никто, включая сам клиент, —
// поэтому строки с этим префиксом отличимы от клиентских и вычищаются отдельно.
const autoIdempotencyKeyPrefix = "auto:"

const (
	idempotencyKeyMinBytes = 8
	idempotencyKeyMaxBytes = 255
)

// resolveIdempotencyKey переводит необязательный заголовок Idempotency-Key в непустой
// ключ для academy: контракт gRPC остаётся безусловным («ключ задан вызывающим»), а
// компенсация отсутствующего HTTP-заголовка — работа адаптера. Присутствующий, но
// некорректный заголовок отклоняется, иначе клиент считал бы себя защищённым от
// дублей, фактически уходя по auto-пути.
func resolveIdempotencyKey(w http.ResponseWriter, value *api.OptionalIdempotencyKey) (string, bool) {
	if value == nil {
		return autoIdempotencyKeyPrefix + uuid.NewString(), true
	}
	key := strings.TrimSpace(string(*value))
	if len(key) < idempotencyKeyMinBytes || len(key) > idempotencyKeyMaxBytes {
		apierror.Write(w, apierror.BadRequest("Заголовок Idempotency-Key должен содержать от 8 до 255 байт"))
		return "", false
	}
	if strings.HasPrefix(key, autoIdempotencyKeyPrefix) {
		apierror.Write(w, apierror.BadRequest("Префикс auto: зарезервирован для ключей, создаваемых сервером"))
		return "", false
	}
	return key, true
}
