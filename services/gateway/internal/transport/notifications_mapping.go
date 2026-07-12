package transport

import (
	"fmt"

	"github.com/google/uuid"
	notificationsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/notifications/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func notificationFromProto(n *notificationsv1.Notification) (api.AppNotification, error) {
	if n == nil {
		return api.AppNotification{}, fmt.Errorf("empty notification")
	}
	id, e := uuid.Parse(n.GetId())
	if e != nil {
		return api.AppNotification{}, e
	}
	u, e := uuid.Parse(n.GetUserId())
	if e != nil {
		return api.AppNotification{}, e
	}
	if n.GetCreatedAt() == nil {
		return api.AppNotification{}, fmt.Errorf("notification createdAt is required")
	}
	types := map[notificationsv1.NotificationType]api.NotificationType{notificationsv1.NotificationType_NOTIFICATION_TYPE_TASK_ASSIGNED: api.NotificationTypeTaskAssigned, notificationsv1.NotificationType_NOTIFICATION_TYPE_TASK_COMMENT: api.NotificationTypeTaskComment, notificationsv1.NotificationType_NOTIFICATION_TYPE_TASK_DUE: api.NotificationTypeTaskDue, notificationsv1.NotificationType_NOTIFICATION_TYPE_ARTICLE_PUBLISHED: api.NotificationTypeArticlePublished, notificationsv1.NotificationType_NOTIFICATION_TYPE_ARTICLE_ACK_REQUIRED: api.NotificationTypeArticleAckRequired, notificationsv1.NotificationType_NOTIFICATION_TYPE_COURSE_ASSIGNED: api.NotificationTypeCourseAssigned, notificationsv1.NotificationType_NOTIFICATION_TYPE_COURSE_DUE: api.NotificationTypeCourseDue, notificationsv1.NotificationType_NOTIFICATION_TYPE_MENTION: api.NotificationTypeMention}
	typ, ok := types[n.GetType()]
	if !ok {
		return api.AppNotification{}, fmt.Errorf("unknown notification type")
	}
	return api.AppNotification{Id: id, UserId: u, Type: typ, Title: n.GetTitle(), Body: n.Body, Link: n.Link, Read: n.GetRead(), CreatedAt: n.GetCreatedAt().AsTime()}, nil
}
