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

	// Minimal payload: only title, body and url to keep push small and simple.
	base := map[string]string{
		"title": title,
		"body":  body,
		"url":   relURL,
	}

	payload, err := json.Marshal(base)

	if err != nil {
		logger.Error(fmt.Sprintf("marshal payload: %v", err))
		return fmt.Errorf("marshal payload: %w", err)
	}

	// Keep a small safety truncation in case caller provided very large body
	// to avoid 413 responses. We keep this conservative but simple.
	const safeLimit = 1800
	if len(payload) > safeLimit {
		maxBody := 500
		runeBody := []rune(body)
		if len(runeBody) > maxBody {
			body = string(runeBody[:maxBody]) + "…"
		}
		small := map[string]string{"title": title, "body": body, "url": relURL}
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
