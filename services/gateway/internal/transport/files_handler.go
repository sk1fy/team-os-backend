package transport

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	filesv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/files/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

const maxUploadBody = 26 << 20

func (h *Handler) UploadFile(w http.ResponseWriter, r *http.Request) {
	if h.files == nil {
		apierror.Write(w, apierror.New(http.StatusServiceUnavailable, "Сервис файлов временно недоступен"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBody)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		apierror.Write(w, apierror.BadRequest("Не удалось прочитать загружаемый файл"))
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		apierror.Write(w, apierror.BadRequest("Файл обязателен"))
		return
	}
	defer func() { _ = file.Close() }()
	purpose, err := filePurposeToProto(api.FilePurpose(strings.TrimSpace(r.FormValue("purpose"))))
	if err != nil {
		apierror.Write(w, apierror.BadRequest(err.Error()))
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	stream, err := h.files.UploadFile(outgoingContext(r))
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	if err = stream.Send(&filesv1.UploadFileRequest{Payload: &filesv1.UploadFileRequest_Info{Info: &filesv1.UploadFileInfo{Name: header.Filename, ContentType: contentType, Size: uint64(header.Size), Purpose: purpose}}}); err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	buffer := make([]byte, 256<<10)
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			chunk := append([]byte(nil), buffer[:count]...)
			if err = stream.Send(&filesv1.UploadFileRequest{Payload: &filesv1.UploadFileRequest_Chunk{Chunk: chunk}}); err != nil {
				h.writeRPCError(w, r, err)
				return
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			apierror.Write(w, apierror.BadRequest("Не удалось прочитать загружаемый файл"))
			return
		}
	}
	response, err := stream.CloseAndRecv()
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := uploadedFileFromProto(response.File)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) GetFile(w http.ResponseWriter, r *http.Request, id api.ID) {
	if h.files == nil {
		apierror.Write(w, apierror.New(http.StatusServiceUnavailable, "Сервис файлов временно недоступен"))
		return
	}
	response, err := h.files.GetFile(outgoingContext(r), &filesv1.GetFileRequest{Id: id.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	file, err := uploadedFileFromProto(response.File)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, api.FileDownload{File: file, DownloadUrl: response.DownloadUrl})
}
func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request, id api.ID) {
	if h.files == nil {
		apierror.Write(w, apierror.New(http.StatusServiceUnavailable, "Сервис файлов временно недоступен"))
		return
	}
	if _, err := h.files.DeleteFile(outgoingContext(r), &filesv1.DeleteFileRequest{Id: id.String()}); err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func filePurposeToProto(value api.FilePurpose) (filesv1.FilePurpose, error) {
	switch value {
	case api.FilePurposeAttachment:
		return filesv1.FilePurpose_FILE_PURPOSE_ATTACHMENT, nil
	case api.FilePurposeAvatar:
		return filesv1.FilePurpose_FILE_PURPOSE_AVATAR, nil
	case api.FilePurposeLogo:
		return filesv1.FilePurpose_FILE_PURPOSE_LOGO, nil
	default:
		return 0, errors.New("Неизвестное назначение файла")
	}
}
func filePurposeFromProto(value filesv1.FilePurpose) (api.FilePurpose, error) {
	switch value {
	case filesv1.FilePurpose_FILE_PURPOSE_ATTACHMENT:
		return api.FilePurposeAttachment, nil
	case filesv1.FilePurpose_FILE_PURPOSE_AVATAR:
		return api.FilePurposeAvatar, nil
	case filesv1.FilePurpose_FILE_PURPOSE_LOGO:
		return api.FilePurposeLogo, nil
	default:
		return "", errors.New("files вернул неизвестное назначение файла")
	}
}
func uploadedFileFromProto(value *filesv1.FileMetadata) (api.UploadedFile, error) {
	if value == nil {
		return api.UploadedFile{}, errors.New("files вернул пустые метаданные")
	}
	id, err := uuid.Parse(value.Id)
	if err != nil {
		return api.UploadedFile{}, err
	}
	purpose, err := filePurposeFromProto(value.Purpose)
	if err != nil {
		return api.UploadedFile{}, err
	}
	created := value.CreatedAt.AsTime()
	return api.UploadedFile{Id: id, Name: value.Name, ContentType: value.ContentType, Size: int64(value.Size), Purpose: purpose, CreatedAt: created}, nil
}
