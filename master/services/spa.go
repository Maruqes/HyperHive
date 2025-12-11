package services

import (
	"512SvMan/db"
	"512SvMan/spa"
	"context"
	"database/sql"
	"errors"
	"fmt"

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
