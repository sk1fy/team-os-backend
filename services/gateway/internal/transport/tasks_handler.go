package transport

import (
	"net/http"

	tasksv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/tasks/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/authmw"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (h *Handler) GetBoards(w http.ResponseWriter, r *http.Request) {
	response, err := h.tasks.GetBoards(outgoingContext(r), &tasksv1.GetBoardsRequest{})
	if err != nil {
		h.writeTasksRPCError(w, r, err)
		return
	}
	converted, err := boardsFromProto(response.GetBoards())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetColumns(w http.ResponseWriter, r *http.Request, boardID api.ID) {
	response, err := h.tasks.GetColumns(outgoingContext(r), &tasksv1.GetColumnsRequest{
		BoardId: boardID.String(),
	})
	if err != nil {
		h.writeTasksRPCError(w, r, err)
		return
	}
	converted, err := taskColumnsFromProto(response.GetColumns())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CreateColumn(w http.ResponseWriter, r *http.Request, boardID api.ID) {
	if !requireBoardManager(w, r) {
		return
	}
	var input api.CreateTaskColumnInput
	if !decode(w, r, &input) {
		return
	}
	request := &tasksv1.CreateColumnRequest{BoardId: boardID.String(), Name: input.Name, Color: input.Color}
	response, err := h.tasks.CreateColumn(outgoingContext(r), request)
	if err != nil {
		h.writeTasksRPCError(w, r, err)
		return
	}
	converted, err := taskColumnFromProto(response.GetColumn())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) UpdateColumn(w http.ResponseWriter, r *http.Request, id api.Id) {
	if !requireBoardManager(w, r) {
		return
	}
	var input api.UpdateTaskColumnInput
	if !decode(w, r, &input) {
		return
	}
	request := &tasksv1.UpdateColumnRequest{Id: id.String(), Name: input.Name, Color: input.Color}
	response, err := h.tasks.UpdateColumn(outgoingContext(r), request)
	if err != nil {
		h.writeTasksRPCError(w, r, err)
		return
	}
	converted, err := taskColumnFromProto(response.GetColumn())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func requireBoardManager(w http.ResponseWriter, r *http.Request) bool {
	claims, ok := authmw.Claims(r.Context())
	if !ok {
		apierror.Write(w, apierror.Unauthorized())
		return false
	}
	if claims.Role != "owner" && claims.Role != "admin" {
		apierror.Write(w, apierror.Forbidden("Недостаточно прав для изменения структуры доски"))
		return false
	}
	return true
}

func (h *Handler) GetTasks(w http.ResponseWriter, r *http.Request, params api.GetTasksParams) {
	request := &tasksv1.GetTasksRequest{}
	if params.BoardId != nil {
		boardID := params.BoardId.String()
		request.BoardId = &boardID
	}
	response, err := h.tasks.GetTasks(outgoingContext(r), request)
	if err != nil {
		h.writeTasksRPCError(w, r, err)
		return
	}
	converted, err := tasksFromProto(response.GetTasks())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request, id api.Id) {
	response, err := h.tasks.GetTask(outgoingContext(r), &tasksv1.GetTaskRequest{Id: id.String()})
	if err != nil {
		h.writeTasksRPCError(w, r, err)
		return
	}
	converted, err := taskFromProto(response.GetTask())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var input api.CreateTaskInput
	if !decode(w, r, &input) {
		return
	}
	request := &tasksv1.CreateTaskRequest{
		BoardId: input.BoardId.String(), ColumnId: input.ColumnId.String(), Title: input.Title,
	}
	if input.Priority != nil {
		priority, err := taskPriorityToProto(*input.Priority)
		if err != nil {
			h.writeConversionError(w, r, err)
			return
		}
		request.Priority = &priority
	}
	response, err := h.tasks.CreateTask(outgoingContext(r), request)
	if err != nil {
		h.writeTasksRPCError(w, r, err)
		return
	}
	converted, err := taskFromProto(response.GetTask())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) UpdateTask(w http.ResponseWriter, r *http.Request, id api.Id) {
	var input api.UpdateTaskInput
	if !decode(w, r, &input) {
		return
	}
	request, err := updateTaskToProto(id.String(), input)
	if err != nil {
		apierror.Write(w, apierror.BadRequest(err.Error()))
		return
	}
	response, err := h.tasks.UpdateTask(outgoingContext(r), request)
	if err != nil {
		h.writeTasksRPCError(w, r, err)
		return
	}
	converted, err := taskFromProto(response.GetTask())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) MoveTask(w http.ResponseWriter, r *http.Request, taskID api.ID) {
	var input api.MoveTaskInput
	if !decode(w, r, &input) {
		return
	}
	if input.Order < 0 {
		apierror.Write(w, apierror.BadRequest("Порядок задачи не может быть отрицательным"))
		return
	}
	response, err := h.tasks.MoveTask(outgoingContext(r), &tasksv1.MoveTaskRequest{
		TaskId: taskID.String(), ColumnId: input.ColumnId.String(), Order: uint32(input.Order),
	})
	if err != nil {
		h.writeTasksRPCError(w, r, err)
		return
	}
	converted, err := taskFromProto(response.GetTask())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetComments(w http.ResponseWriter, r *http.Request, taskID api.ID) {
	response, err := h.tasks.GetComments(outgoingContext(r), &tasksv1.GetCommentsRequest{
		TaskId: taskID.String(),
	})
	if err != nil {
		h.writeTasksRPCError(w, r, err)
		return
	}
	converted, err := taskCommentsFromProto(response.GetComments())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) AddComment(w http.ResponseWriter, r *http.Request, taskID api.ID) {
	var input api.AddTaskCommentInput
	if !decode(w, r, &input) {
		return
	}
	content, err := richTextToStruct(input.Content)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	response, err := h.tasks.AddComment(outgoingContext(r), &tasksv1.AddCommentRequest{
		TaskId: taskID.String(), Content: content,
	})
	if err != nil {
		h.writeTasksRPCError(w, r, err)
		return
	}
	converted, err := taskCommentFromProto(response.GetComment())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) GetLabels(w http.ResponseWriter, r *http.Request) {
	response, err := h.tasks.GetLabels(outgoingContext(r), &tasksv1.GetLabelsRequest{})
	if err != nil {
		h.writeTasksRPCError(w, r, err)
		return
	}
	converted, err := labelsFromProto(response.GetLabels())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) writeTasksRPCError(w http.ResponseWriter, r *http.Request, err error) {
	grpcStatus, ok := status.FromError(err)
	if !ok {
		h.logger.ErrorContext(r.Context(), "tasks RPC failed", "error", err)
		apierror.Write(w, apierror.Internal(err))
		return
	}
	message := grpcStatus.Message()
	switch grpcStatus.Code() {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		apierror.Write(w, apierror.BadRequest(message))
	case codes.Unauthenticated:
		apierror.Write(w, apierror.Unauthorized(message))
	case codes.PermissionDenied:
		apierror.Write(w, apierror.Forbidden(message))
	case codes.NotFound:
		apierror.Write(w, apierror.New(http.StatusNotFound, message))
	case codes.AlreadyExists, codes.Aborted:
		apierror.Write(w, apierror.Conflict(message))
	case codes.Unavailable, codes.DeadlineExceeded:
		h.logger.WarnContext(r.Context(), "tasks RPC unavailable", "code", grpcStatus.Code(), "error", err)
		apierror.Write(w, apierror.New(http.StatusServiceUnavailable, "Сервис временно недоступен"))
	default:
		h.logger.ErrorContext(r.Context(), "tasks RPC failed", "code", grpcStatus.Code(), "error", err)
		apierror.Write(w, apierror.Internal(err))
	}
}
