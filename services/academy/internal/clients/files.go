package clients

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	filesv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/files/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
	"google.golang.org/grpc/metadata"
)

type Files struct {
	client filesv1.FilesServiceClient
}

func NewFiles(client filesv1.FilesServiceClient) *Files { return &Files{client: client} }

var _ application.FilesClient = (*Files)(nil)

func (f *Files) CloneFilesForOwner(
	ctx context.Context,
	companyID, userID uuid.UUID,
	role, idempotencyKey string,
	targetOwnerType string,
	targetOwnerID uuid.UUID,
	sourceFileIDs []uuid.UUID,
) (application.FileCloneResult, error) {
	var ownerType filesv1.FileOwnerType
	switch targetOwnerType {
	case "course_version":
		ownerType = filesv1.FileOwnerType_FILE_OWNER_TYPE_COURSE_VERSION
	case "template_version":
		ownerType = filesv1.FileOwnerType_FILE_OWNER_TYPE_TEMPLATE_VERSION
	default:
		return application.FileCloneResult{}, fmt.Errorf("неизвестный тип владельца файлов %q", targetOwnerType)
	}
	ids := make([]string, len(sourceFileIDs))
	for index := range sourceFileIDs {
		ids[index] = sourceFileIDs[index].String()
	}
	callContext := metadata.AppendToOutgoingContext(
		ctx,
		"x-company-id", companyID.String(),
		"x-user-id", userID.String(),
		"x-user-role", role,
	)
	response, err := f.client.CloneFilesForOwner(callContext, &filesv1.CloneFilesForOwnerRequest{
		SourceFileIds:  ids,
		TargetOwner:    &filesv1.FileOwner{Type: ownerType, Id: targetOwnerID.String()},
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return application.FileCloneResult{}, fmt.Errorf("files.CloneFilesForOwner: %w", err)
	}
	state := "pending"
	switch response.GetState() {
	case filesv1.FileCloneState_FILE_CLONE_STATE_IN_PROGRESS:
		state = "in_progress"
	case filesv1.FileCloneState_FILE_CLONE_STATE_SUCCEEDED:
		state = "succeeded"
	case filesv1.FileCloneState_FILE_CLONE_STATE_FAILED:
		state = "failed"
	}
	result := application.FileCloneResult{State: state, Files: make(map[uuid.UUID]uuid.UUID, len(response.GetFiles()))}
	for _, cloned := range response.GetFiles() {
		sourceID, sourceErr := uuid.Parse(cloned.GetSourceFileId())
		targetID, targetErr := uuid.Parse(cloned.GetFile().GetId())
		if sourceErr != nil || targetErr != nil {
			return application.FileCloneResult{}, fmt.Errorf("files вернул некорректное соответствие файлов")
		}
		result.Files[sourceID] = targetID
	}
	return result, nil
}
