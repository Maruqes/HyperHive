package services

import (
	"512SvMan/db"
	"512SvMan/spa"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Maruqes/512SvMan/logger"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrSPAPortNotFound = errors.New("spa port not found")
	ErrInvalidPassword = errors.New("invalid password")
)

type SPAService struct{}

func (s *SPAService) Create(ctx context.Context, port int, password string) error {
	if password == "" {
		return fmt.Errorf("password is required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	if err := spa.EnableSPA(port); err != nil {
		return err
	}

	if err := db.UpsertSPAPort(ctx, port, string(hash)); err != nil {
		disableErr := spa.DisableSPA(port)
		if disableErr != nil {
			return fmt.Errorf("upsert spa port: %w (rollback disable failed: %v)", err, disableErr)
		}
		return err
	}

	return nil
}

func (s *SPAService) Delete(ctx context.Context, port int) error {
	entry, err := db.GetSPAPort(ctx, port)
	if err != nil {
		return err
	}
	if entry == nil {
		return ErrSPAPortNotFound
	}

	if err := spa.DisableSPA(port); err != nil {
		return err
	}

	if err := db.DeleteSPAPort(ctx, port); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSPAPortNotFound
		}
		reapplyErr := spa.EnableSPA(port)
		if reapplyErr != nil {
			return fmt.Errorf("delete spa port: %w (rollback enable failed: %v)", err, reapplyErr)
		}
		return err
	}

	return nil
}

func (s *SPAService) Allow(ctx context.Context, port int, password, ip string, seconds int) error {
	entry, err := db.GetSPAPort(ctx, port)
	if err != nil {
		return err
	}
	if entry == nil {
		return ErrSPAPortNotFound
	}

	if err := bcrypt.CompareHashAndPassword([]byte(entry.PasswordHash), []byte(password)); err != nil {
		return ErrInvalidPassword
	}

	return spa.AllowIP(port, ip, seconds)
}

func (s *SPAService) List(ctx context.Context) ([]db.SPAPort, error) {
	return db.ListSPAPorts(ctx)
}

func (s *SPAService) ListAllows(ctx context.Context, port int) ([]spa.AllowEntry, error) {
	entry, err := db.GetSPAPort(ctx, port)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, ErrSPAPortNotFound
	}
	return spa.ListAllows(port)
}

func (s *SPAService) Maintain(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Reapply(ctx); err != nil {
				logger.Errorf("reapply SPA rules: %v", err)
			}
		}
	}
}

func (s *SPAService) Reapply(ctx context.Context) error {
	ports, err := db.ListSPAPorts(ctx)
	if err != nil {
		return err
	}
	for _, p := range ports {
		if err := spa.EnableSPA(p.Port); err != nil {
			return fmt.Errorf("enable SPA for port %d: %w", p.Port, err)
		}
	}
	return nil
}
