package nots

import (
	"512SvMan/db"
	"512SvMan/env512"
	"512SvMan/websocket"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/SherClockHolmes/webpush-go"
)

var mailto = "mailto:push.hyperhive@gmail.com"

// WebsocketChannel shows critical notifications as a modal and the rest as toasts.
type WebsocketChannel struct{}

func (WebsocketChannel) Name() string { return "websocket" }

func (WebsocketChannel) Send(_ context.Context, n Notification) error {
	if n.Severity == SeverityCritical {
		// Type "Notification" is the modal message older frontends already handle.
		websocket.BroadcastMessage(websocket.Message{Type: "Notification", Data: n.Body, Extra: n.Title})
		return nil
	}
	data, err := json.Marshal(map[string]string{
		"title":    n.Title,
		"body":     n.Body,
		"severity": n.Severity.String(),
		"event":    n.Event,
		"machine":  n.Machine,
		"url":      n.URL,
	})
	if err != nil {
		return fmt.Errorf("marshal toast: %w", err)
	}
	websocket.BroadcastMessage(websocket.Message{Type: "NotificationToast", Data: string(data), Extra: n.Severity.String()})
	return nil
}

// WebPushChannel sends to every stored browser subscription.
type WebPushChannel struct{}

func (WebPushChannel) Name() string { return "webpush" }

func (WebPushChannel) Send(ctx context.Context, n Notification) error {
	subs, err := db.DbGetAllSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("load subscriptions: %w", err)
	}
	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Add(1)
		go func(sub db.PushSubscription) {
			defer wg.Done()
			err := SendWebPush(ctx, sub, n)
			if errors.Is(err, errSubscriptionGone) {
				if delErr := db.DbDeleteSubscription(ctx, sub.Endpoint); delErr != nil {
					logger.Errorf("delete expired push subscription: %v", delErr)
				}
				return
			}
			if err != nil {
				logger.Errorf("web push to %s: %v", sub.Endpoint, err)
			}
		}(sub)
	}
	wg.Wait()
	return nil
}

var errSubscriptionGone = errors.New("push subscription expired or unsubscribed")

// SendWebPush sends one notification to one browser subscription.
func SendWebPush(ctx context.Context, sub db.PushSubscription, n Notification) error {
	if env512.VapidPublicKey == "" || env512.VapidPrivateKey == "" {
		return fmt.Errorf("VAPID keys not set; call InitVAPIDFromEnv() at startup")
	}

	body := n.Body
	build := func() ([]byte, error) {
		base := map[string]string{
			"title":    n.Title,
			"body":     body,
			"url":      n.URL,
			"icon":     "/static/notification-icon.png",
			"severity": n.Severity.String(),
		}
		if n.Severity == SeverityCritical {
			base["critical"] = "true"
		}
		return json.Marshal(base)
	}
	payload, err := build()
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	const safeLimit = 1500
	if len(payload) > safeLimit {
		if runes := []rune(body); len(runes) > 300 {
			body = string(runes[:300]) + "…"
		}
		if payload, err = build(); err != nil {
			return fmt.Errorf("marshal small payload: %w", err)
		}
	}

	subscription := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.Keys.P256dh,
			Auth:   sub.Keys.Auth,
		},
	}
	urgency := webpush.UrgencyNormal
	if n.Severity >= SeverityError {
		urgency = webpush.UrgencyHigh
	}
	resp, err := webpush.SendNotificationWithContext(ctx, payload, subscription, &webpush.Options{
		Subscriber:      mailto,
		VAPIDPublicKey:  env512.VapidPublicKey,
		VAPIDPrivateKey: env512.VapidPrivateKey,
		TTL:             60,
		RecordSize:      3000,
		Urgency:         urgency,
	})
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return errSubscriptionGone
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("webpush error status=%d body=%s", resp.StatusCode, string(b))
	}
	return nil
}

// FCMChannel sends to registered Android devices of the backend's Firebase project.
type FCMChannel struct{}

func (FCMChannel) Name() string { return "fcm" }

func (FCMChannel) Send(ctx context.Context, n Notification) error {
	sender, err := NewFirebaseSender()
	if errors.Is(err, ErrFirebaseUnavailable) {
		return nil // Android push is optional; the backend has no service account.
	}
	if err != nil {
		return err
	}
	devices, err := db.DbGetFirebaseDevices(ctx, sender.ProjectID())
	if err != nil {
		return fmt.Errorf("load firebase devices: %w", err)
	}
	return sendToDevices(ctx, sender, devices, n, db.DbDeleteFirebaseDevice)
}

type fcmSender interface {
	SendNotification(ctx context.Context, token string, n Notification) (string, error)
}

// sendToDevices sends to each device and removes tokens FCM reports as unregistered.
func sendToDevices(ctx context.Context, sender fcmSender, devices []db.FirebaseDevice, n Notification, remove func(context.Context, string) error) error {
	var wg sync.WaitGroup
	for _, device := range devices {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			_, err := sender.SendNotification(ctx, token, n)
			var providerErr *FirebaseError
			if errors.As(err, &providerErr) && providerErr.Unregistered {
				if delErr := remove(ctx, token); delErr != nil {
					logger.Errorf("delete unregistered firebase device: %v", delErr)
				}
				return
			}
			if err != nil {
				// Tokens are credentials for the device; never log them.
				logger.Errorf("firebase notification: %v", err)
			}
		}(device.Token)
	}
	wg.Wait()
	return nil
}
