package services

import (
	"512SvMan/db"
	"512SvMan/env512"
	"encoding/json"
	"fmt"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/SherClockHolmes/webpush-go"
)

type NotsService struct{}

func (s *NotsService) SendWebPush(sub db.PushSubscription, title, body, relURL string, critical bool) error {
	if env512.VapidPublicKey == "" || env512.VapidPrivateKey == "" {
		logger.Error("VAPID keys not set; call InitVAPIDFromEnv() at startup")
		return fmt.Errorf("VAPID keys not set; call InitVAPIDFromEnv() at startup")
	}

	// Include optional icon/badge paths so the client Service Worker
	// can display a custom notification logo. These files should be
	// served from the `/static/` path (add them to the static folder).

	sev := "info"
	if critical {
		sev = "critical"
	}
	base := map[string]string{
		"title":    title,
		"body":     body,
		"url":      relURL,
		"icon":     "/static/notification-icon.png",
		"badge":    "/static/notification-badge.png",
		"severity": sev, // ou "info"/"warning" conforme o tipo de alerta
	}

	payload, err := json.Marshal(base)

	if err != nil {
		logger.Error(fmt.Sprintf("marshal payload: %v", err))
		return fmt.Errorf("marshal payload: %w", err)
	}

	// Protect against push services rejecting large payloads (HTTP 413).
	// Many push services (FCM/Chrome) limit the encrypted payload to ~4KB;
	// after encryption overhead the safe payload size is smaller. If the
	// payload is large, send a trimmed payload to avoid 413 errors.
	const safeLimit = 1800 // bytes - conservative threshold
	if len(payload) > safeLimit {
		// truncate body to reduce size and remove optional fields
		maxBody := 1000
		runeBody := []rune(body)
		if len(runeBody) > maxBody {
			body = string(runeBody[:maxBody]) + "…"
		}

		// create a minimal payload (no icon/badge/severity) to stay small
		small := map[string]string{
			"title": title,
			"body":  body,
			"url":   relURL,
			"note":  "truncated",
		}
		payload, err = json.Marshal(small)
		if err != nil {
			logger.Error(fmt.Sprintf("marshal small payload: %v", err))
			return fmt.Errorf("marshal small payload: %w", err)
		}
		logger.Error(fmt.Sprintf("payload too large, sent truncated notification (size=%d)", len(payload)))
	}

	subscription := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.Keys.P256dh,
			Auth:   sub.Keys.Auth,
		},
	}

	resp, err := webpush.SendNotification(payload, subscription, &webpush.Options{
		Subscriber:      "mailto:noreply@hyperhive.local",
		VAPIDPublicKey:  env512.VapidPublicKey,
		VAPIDPrivateKey: env512.VapidPrivateKey,
		TTL:             60,
	})
	if err != nil {
		logger.Error(fmt.Sprintf("send notification: %v", err))
		return fmt.Errorf("send notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		logger.Error(fmt.Sprintf("webpush error status=%d", resp.StatusCode))
		return fmt.Errorf("webpush error status=%d", resp.StatusCode)
	}

	return nil
}

// Envia notificação global (para TODOS os subs)
func (s *NotsService) SendGlobalNotification(title, body, relURL string) error {
	subs, err := db.DbGetAllSubscriptions()
	if err != nil {
		logger.Error(fmt.Sprintf("load subs: %v", err))
		return err
	}

	for _, sub := range subs {
		go s.SendWebPush(sub, title, body, relURL, true)
	}
	return nil
}
