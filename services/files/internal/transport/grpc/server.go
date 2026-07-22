package grpc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/google/uuid"
	filesv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/files/v1"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/services/files/internal/application"
	"github.com/sk1fy/team-os-backend/services/files/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxChunkSize = 1 << 20

type Server struct {
	filesv1.UnimplementedFilesServiceServer
	service *application.Service
	auth    actorResolver
	tempDir string
}

func New(service *application.Service, verifier *sharedauth.TokenVerifier, trustedMetadata bool, tempDir string) *Server {
	return &Server{service: service, auth: actorResolver{verifier: verifier, trustedMetadata: trustedMetadata}, tempDir: tempDir}
}

func (s *Server) UploadFile(stream filesv1.FilesService_UploadFileServer) error {
	actor, err := s.auth.actor(stream.Context())
	if err != nil {
		return err
	}
	first, err := stream.Recv()
	if err != nil {
		return status.Error(codes.InvalidArgument, "Не переданы метаданные файла")
	}
	info := first.GetInfo()
	if info == nil {
		return status.Error(codes.InvalidArgument, "Первое сообщение должно содержать метаданные файла")
	}
	upload := domain.Upload{Name: info.GetName(), ContentType: info.GetContentType(), Size: int64(info.GetSize()), Purpose: fromProtoPurpose(info.GetPurpose())}
	if err = s.service.Validate(upload); err != nil {
		return domainError(err)
	}
	tmp, err := os.CreateTemp(s.tempDir, "teamos-upload-*")
	if err != nil {
		return status.Error(codes.Internal, "Не удалось подготовить загрузку файла")
	}
	name := tmp.Name()
	defer func() { _ = tmp.Close(); _ = os.Remove(name) }()
	var written int64
	for {
		message, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return status.Error(codes.InvalidArgument, "Не удалось прочитать файл")
		}
		chunk := message.GetChunk()
		if chunk == nil {
			return status.Error(codes.InvalidArgument, "Метаданные файла можно передать только один раз")
		}
		if len(chunk) > maxChunkSize {
			return status.Error(codes.ResourceExhausted, "Слишком большой фрагмент файла")
		}
		written += int64(len(chunk))
		if written > upload.Size {
			return status.Error(codes.InvalidArgument, "Размер файла не соответствует заявленному")
		}
		if _, err = tmp.Write(chunk); err != nil {
			return status.Error(codes.Internal, "Не удалось сохранить загружаемый файл")
		}
	}
	if written != upload.Size {
		return status.Error(codes.InvalidArgument, "Размер файла не соответствует заявленному")
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return status.Error(codes.Internal, "Не удалось проверить загружаемый файл")
	}
	header := make([]byte, min(512, int(upload.Size)))
	if _, err = io.ReadFull(tmp, header); err != nil {
		return status.Error(codes.Internal, "Не удалось проверить загружаемый файл")
	}
	if err = domain.ValidateDetectedType(upload.ContentType, http.DetectContentType(header)); err != nil {
		return domainError(err)
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return status.Error(codes.Internal, "Не удалось обработать загружаемый файл")
	}
	f, err := s.service.Upload(stream.Context(), actor.companyID, actor.userID, actor.role, upload, tmp)
	if err != nil {
		return domainError(err)
	}
	return stream.SendAndClose(&filesv1.UploadFileResponse{File: toProto(f)})
}
func (s *Server) GetFile(ctx context.Context, req *filesv1.GetFileRequest) (*filesv1.GetFileResponse, error) {
	actor, err := s.auth.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Некорректный идентификатор файла")
	}
	f, url, err := s.service.Get(ctx, actor.companyID, id)
	if err != nil {
		return nil, domainError(err)
	}
	return &filesv1.GetFileResponse{File: toProto(f), DownloadUrl: url}, nil
}
func (s *Server) DeleteFile(ctx context.Context, req *filesv1.DeleteFileRequest) (*filesv1.DeleteFileResponse, error) {
	actor, err := s.auth.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Некорректный идентификатор файла")
	}
	if err = s.service.Delete(ctx, actor.companyID, actor.userID, actor.role, id); err != nil {
		return nil, domainError(err)
	}
	return &filesv1.DeleteFileResponse{}, nil
}

func (s *Server) CloneFilesForOwner(ctx context.Context, req *filesv1.CloneFilesForOwnerRequest) (*filesv1.CloneFilesForOwnerResponse, error) {
	actor, err := s.auth.actor(ctx)
	if err != nil {
		return nil, err
	}
	target, err := ownerFromProto(req.GetTargetOwner())
	if err != nil {
		return nil, err
	}
	sourceIDs := make([]uuid.UUID, 0, len(req.GetSourceFileIds()))
	for _, rawID := range req.GetSourceFileIds() {
		id, parseErr := uuid.Parse(rawID)
		if parseErr != nil {
			return nil, status.Error(codes.InvalidArgument, "Некорректный идентификатор исходного файла")
		}
		sourceIDs = append(sourceIDs, id)
	}
	operation, err := s.service.CloneFilesForOwner(
		ctx, actor.companyID, actor.userID, actor.role,
		req.GetIdempotencyKey(), target, sourceIDs,
	)
	if err != nil {
		return nil, domainError(err)
	}
	return cloneOperationToProto(operation), nil
}

func domainError(err error) error {
	switch {
	case errors.Is(err, application.ErrNotFound):
		return status.Error(codes.NotFound, application.ErrNotFound.Error())
	case errors.Is(err, application.ErrForbidden), errors.Is(err, application.ErrUploadForbidden), errors.Is(err, application.ErrCloneForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, application.ErrIdempotencyKey):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, application.ErrInvalidClone):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrFileTooLarge):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, domain.ErrInvalidName), errors.Is(err, domain.ErrInvalidSize), errors.Is(err, domain.ErrContentTypeDenied), errors.Is(err, domain.ErrContentTypeMismatch), errors.Is(err, domain.ErrInvalidPurpose):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, "Не удалось обработать файл")
	}
}

func ownerFromProto(value *filesv1.FileOwner) (domain.FileOwner, error) {
	if value == nil {
		return domain.FileOwner{}, status.Error(codes.InvalidArgument, "Укажите владельца копий файлов")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return domain.FileOwner{}, status.Error(codes.InvalidArgument, "Некорректный идентификатор владельца файлов")
	}
	ownerType := domain.OwnerType("")
	switch value.GetType() {
	case filesv1.FileOwnerType_FILE_OWNER_TYPE_COURSE_VERSION:
		ownerType = domain.OwnerCourseVersion
	case filesv1.FileOwnerType_FILE_OWNER_TYPE_TEMPLATE_VERSION:
		ownerType = domain.OwnerTemplateVersion
	case filesv1.FileOwnerType_FILE_OWNER_TYPE_ARTICLE_VERSION:
		ownerType = domain.OwnerArticleVersion
	default:
		return domain.FileOwner{}, status.Error(codes.InvalidArgument, "Некорректный тип владельца файлов")
	}
	return domain.FileOwner{Type: ownerType, ID: id}, nil
}

func ownerToProto(value domain.FileOwner) *filesv1.FileOwner {
	ownerType := filesv1.FileOwnerType_FILE_OWNER_TYPE_UNSPECIFIED
	switch value.Type {
	case domain.OwnerCourseVersion:
		ownerType = filesv1.FileOwnerType_FILE_OWNER_TYPE_COURSE_VERSION
	case domain.OwnerTemplateVersion:
		ownerType = filesv1.FileOwnerType_FILE_OWNER_TYPE_TEMPLATE_VERSION
	case domain.OwnerArticleVersion:
		ownerType = filesv1.FileOwnerType_FILE_OWNER_TYPE_ARTICLE_VERSION
	}
	return &filesv1.FileOwner{Type: ownerType, Id: value.ID.String()}
}

func cloneOperationToProto(value domain.CloneOperation) *filesv1.CloneFilesForOwnerResponse {
	state := filesv1.FileCloneState_FILE_CLONE_STATE_UNSPECIFIED
	switch value.State {
	case domain.ClonePending:
		state = filesv1.FileCloneState_FILE_CLONE_STATE_PENDING
	case domain.CloneInProgress:
		state = filesv1.FileCloneState_FILE_CLONE_STATE_IN_PROGRESS
	case domain.CloneSucceeded:
		state = filesv1.FileCloneState_FILE_CLONE_STATE_SUCCEEDED
	case domain.CloneFailed:
		state = filesv1.FileCloneState_FILE_CLONE_STATE_FAILED
	}
	files := make([]*filesv1.ClonedFile, 0, len(value.Files))
	for _, file := range value.Files {
		files = append(files, &filesv1.ClonedFile{
			SourceFileId: file.SourceFileID.String(), File: toProto(file.File),
		})
	}
	response := &filesv1.CloneFilesForOwnerResponse{
		OperationId: value.ID.String(), State: state,
		TargetOwner: ownerToProto(value.TargetOwner), Files: files,
		UpdatedAt: timestamppb.New(value.UpdatedAt),
	}
	if value.ErrorMessage != "" {
		response.ErrorMessage = &value.ErrorMessage
	}
	return response
}
func fromProtoPurpose(v filesv1.FilePurpose) domain.Purpose {
	switch v {
	case filesv1.FilePurpose_FILE_PURPOSE_ATTACHMENT:
		return domain.PurposeAttachment
	case filesv1.FilePurpose_FILE_PURPOSE_AVATAR:
		return domain.PurposeAvatar
	case filesv1.FilePurpose_FILE_PURPOSE_LOGO:
		return domain.PurposeLogo
	default:
		return ""
	}
}
func toProto(f domain.File) *filesv1.FileMetadata {
	p := filesv1.FilePurpose_FILE_PURPOSE_UNSPECIFIED
	switch f.Purpose {
	case domain.PurposeAttachment:
		p = filesv1.FilePurpose_FILE_PURPOSE_ATTACHMENT
	case domain.PurposeAvatar:
		p = filesv1.FilePurpose_FILE_PURPOSE_AVATAR
	case domain.PurposeLogo:
		p = filesv1.FilePurpose_FILE_PURPOSE_LOGO
	}
	return &filesv1.FileMetadata{Id: f.ID.String(), CompanyId: f.CompanyID.String(), UploadedBy: f.UploadedBy.String(), Name: f.Name, ContentType: f.ContentType, Size: uint64(f.Size), Purpose: p, CreatedAt: timestamppb.New(f.CreatedAt)}
}
