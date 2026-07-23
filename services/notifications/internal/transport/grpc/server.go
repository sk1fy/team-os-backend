package grpc

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	notificationsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/notifications/v1"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	notificationsv1.UnimplementedNotificationsServiceServer
	service  *application.Service
	verifier *sharedauth.TokenVerifier
}

func New(s *application.Service, v *sharedauth.TokenVerifier) *Server {
	return &Server{service: s, verifier: v}
}
func (s *Server) actor(ctx context.Context) (uuid.UUID, uuid.UUID, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return uuid.Nil, uuid.Nil, status.Error(codes.Unauthenticated, "Требуется авторизация")
	}
	v := md.Get("authorization")
	if len(v) != 1 {
		return uuid.Nil, uuid.Nil, status.Error(codes.Unauthenticated, "Требуется авторизация")
	}
	p := strings.Fields(v[0])
	if len(p) != 2 || !strings.EqualFold(p[0], "Bearer") {
		return uuid.Nil, uuid.Nil, status.Error(codes.Unauthenticated, "Некорректный заголовок авторизации")
	}
	c, e := s.verifier.Verify(p[1])
	if e != nil {
		return uuid.Nil, uuid.Nil, status.Error(codes.Unauthenticated, "Токен недействителен или истёк")
	}
	u, e := uuid.Parse(c.Subject)
	if e != nil {
		return uuid.Nil, uuid.Nil, status.Error(codes.Unauthenticated, "Токен недействителен или истёк")
	}
	co, e := uuid.Parse(c.CompanyID)
	if e != nil {
		return uuid.Nil, uuid.Nil, status.Error(codes.Unauthenticated, "Токен недействителен или истёк")
	}
	return u, co, nil
}
func (s *Server) GetNotifications(ctx context.Context, _ *notificationsv1.GetNotificationsRequest) (*notificationsv1.GetNotificationsResponse, error) {
	u, c, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	list, e := s.service.List(ctx, c, u)
	if e != nil {
		return nil, status.Error(codes.Internal, "Не удалось получить уведомления")
	}
	out := make([]*notificationsv1.Notification, 0, len(list))
	for _, n := range list {
		out = append(out, toProto(n))
	}
	return &notificationsv1.GetNotificationsResponse{Notifications: out}, nil
}
func (s *Server) GetUnreadCount(ctx context.Context, _ *notificationsv1.GetUnreadCountRequest) (*notificationsv1.GetUnreadCountResponse, error) {
	u, c, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	n, e := s.service.Count(ctx, c, u)
	if e != nil {
		return nil, status.Error(codes.Internal, "Не удалось получить число уведомлений")
	}
	return &notificationsv1.GetUnreadCountResponse{Count: n}, nil
}
func (s *Server) MarkRead(ctx context.Context, r *notificationsv1.MarkReadRequest) (*notificationsv1.MarkReadResponse, error) {
	u, c, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	id, e := uuid.Parse(r.GetId())
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "Некорректный идентификатор уведомления")
	}
	if e = s.service.MarkRead(ctx, c, u, id); e != nil {
		return nil, status.Error(codes.NotFound, "Уведомление не найдено")
	}
	return &notificationsv1.MarkReadResponse{}, nil
}
func (s *Server) MarkAllRead(ctx context.Context, _ *notificationsv1.MarkAllReadRequest) (*notificationsv1.MarkAllReadResponse, error) {
	u, c, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	if e = s.service.MarkAllRead(ctx, c, u); e != nil {
		return nil, status.Error(codes.Internal, "Не удалось отметить уведомления")
	}
	return &notificationsv1.MarkAllReadResponse{}, nil
}
func (s *Server) StreamNotifications(_ *notificationsv1.StreamNotificationsRequest, stream notificationsv1.NotificationsService_StreamNotificationsServer) error {
	u, _, e := s.actor(stream.Context())
	if e != nil {
		return e
	}
	ch, cancel := s.service.Subscribe(u)
	defer cancel()
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case id := <-ch:
			if e := stream.Send(&notificationsv1.StreamNotificationsResponse{NotificationId: id.String()}); e != nil {
				return e
			}
		}
	}
}
func toProto(n application.Notification) *notificationsv1.Notification {
	p := &notificationsv1.Notification{Id: n.ID.String(), UserId: n.UserID.String(), Type: notificationType(n.Type), Title: n.Title, Read: n.Read, CreatedAt: timestamppb.New(n.CreatedAt)}
	p.Body = n.Body
	p.Link = n.Link
	return p
}
func notificationType(v string) notificationsv1.NotificationType {
	m := map[string]notificationsv1.NotificationType{"task_assigned": notificationsv1.NotificationType_NOTIFICATION_TYPE_TASK_ASSIGNED, "task_comment": notificationsv1.NotificationType_NOTIFICATION_TYPE_TASK_COMMENT, "task_due": notificationsv1.NotificationType_NOTIFICATION_TYPE_TASK_DUE, "article_published": notificationsv1.NotificationType_NOTIFICATION_TYPE_ARTICLE_PUBLISHED, "article_ack_required": notificationsv1.NotificationType_NOTIFICATION_TYPE_ARTICLE_ACK_REQUIRED, "course_assigned": notificationsv1.NotificationType_NOTIFICATION_TYPE_COURSE_ASSIGNED, "course_due": notificationsv1.NotificationType_NOTIFICATION_TYPE_COURSE_DUE, "mention": notificationsv1.NotificationType_NOTIFICATION_TYPE_MENTION, "course_published": notificationsv1.NotificationType_NOTIFICATION_TYPE_COURSE_PUBLISHED, "course_restriction": notificationsv1.NotificationType_NOTIFICATION_TYPE_COURSE_RESTRICTION}
	return m[v]
}

var _ = fmt.Sprintf
