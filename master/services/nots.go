// services/nots.go
package services

import (
	"512SvMan/db"
	"512SvMan/env512"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/SherClockHolmes/webpush-go"
)

// NotsService envia push notifications Web Push simples.
type NotsService struct{}

// SendWebPush envia uma notificação simples (title, body, url) para 1 subscrição.
// Se `critical` for true, a payload inclui essa flag para o SW tratar com vibração/renotify.
func (s *NotsService) SendWebPush(sub db.PushSubscription, title, body, relURL string, critical bool) (err error) {

	if env512.VapidPublicKey == "" || env512.VapidPrivateKey == "" {
		err := fmt.Errorf("VAPID keys not set; call InitVAPIDFromEnv() at startup")
		logger.Error(err.Error())
		return err
	}

	// payload minimal – só o essencial, incluir ícone estático
	base := map[string]string{
		"title": title,
		"body":  body,
		"url":   relURL,
		"icon":  "/static/notification-icon.png",
	}
	if critical {
		base["critical"] = "true"
	}

	payload, err := json.Marshal(base)
	if err != nil {
		logger.Error(fmt.Sprintf("marshal payload: %v", err))
		return fmt.Errorf("marshal payload: %w", err)
	}

	// Pequeno safety para não mandar textos gigantes.
	const safeLimit = 1500
	if len(payload) > safeLimit {
		runes := []rune(body)
		if len(runes) > 300 {
			body = string(runes[:300]) + "…"
		}
		small := map[string]string{
			"title": title,
			"body":  body,
			"url":   relURL,
			"icon":  "/static/notification-icon.png",
		}
		if critical {
			small["critical"] = "true"
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

		// IMPORTANTE: reduzir para caber no Firefox Android (evitar 413).
		RecordSize: 3000, // se quiseres ser extra seguro, usa 2900
	})
	if err != nil {
		logger.Error(fmt.Sprintf("send notification: %v", err))
		return fmt.Errorf("send notification: %w", err)
	}
	defer resp.Body.Close()

	// (opcional) logar corpo em caso de erro para debug
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		logger.Error(fmt.Sprintf("webpush error status=%d body=%s", resp.StatusCode, string(b)))
		return fmt.Errorf("webpush error status=%d", resp.StatusCode)
	}

	return nil
}

// SendGlobalNotification envia uma notificação simples para TODAS as subscrições.
func (s *NotsService) SendGlobalNotification(title, body, relURL string, critical bool) (err error) {
	defer func() {
		// Here "err" refers to the named return variable
		if err == nil {
			errDb := db.DbSaveNot(db.Not{
				Title:     title,
				Body:      body,
				RelURL:    relURL,
				Critical:  critical,
				CreatedAt: time.Now(),
			})
			if errDb != nil {
				logger.Error("could not save notification into db :D hehe")
			}
		}
	}()

	subs, err := db.DbGetAllSubscriptions()
	if err != nil {
		logger.Error(fmt.Sprintf("load subs: %v", err))
		return err
	}

	for _, sub := range subs {
		// se quiseres serial, tira o go
		go func(sub db.PushSubscription) {
			if err := s.SendWebPush(sub, title, body, relURL, critical); err != nil {
				logger.Error(fmt.Sprintf("failed to send to %s: %v", sub.Endpoint, err))
			}
		}(sub)
	}
	return nil
}

// GetNotsSince returns nots created at or after `since`.
func (s *NotsService) GetNotsSince(since time.Time) ([]db.Not, error) {
	return db.DbGetNotsFrom(since)
}
