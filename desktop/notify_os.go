//go:build desktop

package main

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/notify"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// osNotifier delivers watch alerts through the operating system.
type osNotifier struct {
	service *notifications.NotificationService
	seq     atomic.Uint64
}

func (n *osNotifier) Name() string { return "os" }

func (n *osNotifier) Notify(ctx context.Context, msg notify.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.service == nil {
		return fmt.Errorf("os notifications are unavailable")
	}
	id := n.seq.Add(1)
	return n.service.SendNotification(notifications.NotificationOptions{
		ID:    fmt.Sprintf("cloudripper-watch-%d", id),
		Title: msg.Subject,
		Body:  msg.Body,
	})
}
