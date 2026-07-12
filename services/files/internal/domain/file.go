package domain

import (
	"errors"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Purpose string

const (
	PurposeAttachment Purpose = "attachment"
	PurposeAvatar     Purpose = "avatar"
	PurposeLogo       Purpose = "logo"
)

var (
	ErrInvalidName         = errors.New("Некорректное имя файла")
	ErrInvalidSize         = errors.New("Некорректный размер файла")
	ErrFileTooLarge        = errors.New("Файл превышает допустимый размер")
	ErrContentTypeDenied   = errors.New("Тип файла не поддерживается")
	ErrContentTypeMismatch = errors.New("Содержимое файла не соответствует заявленному типу")
	ErrInvalidPurpose      = errors.New("Некорректное назначение файла")
)

type File struct {
	ID, CompanyID, UploadedBy    uuid.UUID
	ObjectKey, Name, ContentType string
	Size                         int64
	Purpose                      Purpose
	CreatedAt                    time.Time
}

type Upload struct {
	Name, ContentType string
	Size              int64
	Purpose           Purpose
}

func ValidateUpload(v Upload, maxSize int64, allowed map[string]struct{}) error {
	name := strings.TrimSpace(v.Name)
	if name == "" || len(name) > 255 || filepath.Base(name) != name || strings.ContainsAny(name, "\x00/\\") {
		return ErrInvalidName
	}
	if v.Size <= 0 {
		return ErrInvalidSize
	}
	if v.Size > maxSize {
		return ErrFileTooLarge
	}
	contentType, _, err := mime.ParseMediaType(v.ContentType)
	if err != nil {
		return ErrContentTypeDenied
	}
	if _, ok := allowed[strings.ToLower(contentType)]; !ok {
		return ErrContentTypeDenied
	}
	switch v.Purpose {
	case PurposeAttachment:
	case PurposeAvatar, PurposeLogo:
		if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
			return ErrContentTypeDenied
		}
	default:
		return ErrInvalidPurpose
	}
	return nil
}

func ValidateDetectedType(declared, detected string) error {
	d, _, err := mime.ParseMediaType(declared)
	if err != nil {
		return ErrContentTypeMismatch
	}
	detected, _, _ = mime.ParseMediaType(detected)
	if detected == "application/zip" && strings.HasPrefix(d, "application/vnd.openxmlformats-officedocument.") {
		return nil
	}
	if d == "text/plain" && detected == "application/octet-stream" {
		return nil
	}
	if !strings.EqualFold(d, detected) {
		return ErrContentTypeMismatch
	}
	return nil
}
