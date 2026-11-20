package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/TomasSorgetti/auto-factura-api/internal/db/sqlc"
)

type adminUser struct {
	ID                string
	Email             string
	PasswordHash      string
	TOTPSecretEnc     []byte
	TOTPEnabled       bool
	SessionsRevokedAt *time.Time
}

func toAdminUser(u sqlc.AdminUser) *adminUser {
	return &adminUser{
		ID:                u.ID.String(),
		Email:             u.Email,
		PasswordHash:      u.PasswordHash,
		TOTPSecretEnc:     u.TotpSecretEnc,
		TOTPEnabled:       u.TotpEnabledAt.Valid,
		SessionsRevokedAt: timePtr(u.SessionsRevokedAt),
	}
}

func (s *Store) userByEmail(ctx context.Context, email string) (*adminUser, error) {
	u, err := s.q.GetAdminUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get admin user by email: %w", err)
	}
	return toAdminUser(u), nil
}

func (s *Store) userByID(ctx context.Context, id string) (*adminUser, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return nil, ErrNotFound
	}
	u, err := s.q.GetAdminUserByID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get admin user: %w", err)
	}
	return toAdminUser(u), nil
}

// enableTOTP returns false when TOTP was already enabled.
func (s *Store) enableTOTP(ctx context.Context, userID string, sealed []byte, step int64) (bool, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return false, err
	}
	n, err := s.q.EnableAdminTOTP(ctx, sqlc.EnableAdminTOTPParams{ID: uid, TotpSecretEnc: sealed, TotpLastStep: &step})
	if err != nil {
		return false, fmt.Errorf("enable admin totp: %w", err)
	}
	return n == 1, nil
}

// advanceTOTPStep returns false when step was already used.
func (s *Store) advanceTOTPStep(ctx context.Context, userID string, step int64) (bool, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return false, err
	}
	n, err := s.q.AdvanceAdminTOTPStep(ctx, sqlc.AdvanceAdminTOTPStepParams{ID: uid, TotpLastStep: &step})
	if err != nil {
		return false, fmt.Errorf("advance totp step: %w", err)
	}
	return n == 1, nil
}

func (s *Store) replaceRecoveryCodes(ctx context.Context, userID string, hashes [][]byte) error {
	uid, err := parseUUID(userID)
	if err != nil {
		return err
	}
	if err := s.q.DeleteRecoveryCodes(ctx, uid); err != nil {
		return fmt.Errorf("delete recovery codes: %w", err)
	}
	for _, h := range hashes {
		if err := s.q.InsertRecoveryCode(ctx, sqlc.InsertRecoveryCodeParams{UserID: uid, CodeHash: h}); err != nil {
			return fmt.Errorf("insert recovery code: %w", err)
		}
	}
	return nil
}

// useRecoveryCode returns false when no unused code matches.
func (s *Store) useRecoveryCode(ctx context.Context, userID string, hash []byte) (bool, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return false, err
	}
	n, err := s.q.UseRecoveryCode(ctx, sqlc.UseRecoveryCodeParams{UserID: uid, CodeHash: hash})
	if err != nil {
		return false, fmt.Errorf("use recovery code: %w", err)
	}
	return n == 1, nil
}

func (s *Store) unusedRecoveryCodes(ctx context.Context, userID string) (int, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return 0, err
	}
	n, err := s.q.CountUnusedRecoveryCodes(ctx, uid)
	if err != nil {
		return 0, fmt.Errorf("count recovery codes: %w", err)
	}
	return int(n), nil
}
