package transport

import (
	"fmt"
	"net/http"

	notificationsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/notifications/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func (h *Handler) notificationsClient(w http.ResponseWriter) notificationsv1.NotificationsServiceClient {
	if h.notifications == nil {
		http.Error(w, "Сервис уведомлений временно недоступен", http.StatusServiceUnavailable)
		return nil
	}
	return h.notifications
}
func (h *Handler) GetNotifications(w http.ResponseWriter, r *http.Request) {
	c := h.notificationsClient(w)
	if c == nil {
		return
	}
	res, e := c.GetNotifications(outgoingContext(r), &notificationsv1.GetNotificationsRequest{})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	out := make([]api.AppNotification, 0, len(res.GetNotifications()))
	for _, n := range res.GetNotifications() {
		v, e := notificationFromProto(n)
		if e != nil {
			h.writeConversionError(w, r, e)
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *Handler) GetUnreadCount(w http.ResponseWriter, r *http.Request) {
	c := h.notificationsClient(w)
	if c == nil {
		return
	}
	res, e := c.GetUnreadCount(outgoingContext(r), &notificationsv1.GetUnreadCountRequest{})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	writeJSON(w, http.StatusOK, int(res.GetCount()))
}
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request, id api.Id) {
	c := h.notificationsClient(w)
	if c == nil {
		return
	}
	_, e := c.MarkRead(outgoingContext(r), &notificationsv1.MarkReadRequest{Id: id.String()})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	c := h.notificationsClient(w)
	if c == nil {
		return
	}
	_, e := c.MarkAllRead(outgoingContext(r), &notificationsv1.MarkAllReadRequest{})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) StreamNotifications(w http.ResponseWriter, r *http.Request) {
	c := h.notificationsClient(w)
	if c == nil {
		return
	}
	stream, e := c.StreamNotifications(outgoingContext(r), &notificationsv1.StreamNotificationsRequest{})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE недоступен", 500)
		return
	}
	for {
		event, e := stream.Recv()
		if e != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "event: notification\ndata: {\"id\":%q}\n\n", event.GetNotificationId())
		f.Flush()
	}
}
