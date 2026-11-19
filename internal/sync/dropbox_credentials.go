package sync

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Authenticate renews a stale access token without waiting for a storage 401.
func (b *DropboxBackend) Authenticate() error {
	_, err := b.accessToken(b.ctx, nil)
	return err
}

func (b *DropboxBackend) IsAuthenticated() bool {
	return !b.needsRefresh(b.token.Load())
}

func (b *DropboxBackend) needsRefresh(t *DropboxToken) bool {
	now := time.Now()
	if t != nil && t.AccessToken != "" && (b.oauth == nil || b.oauth.AppKey == "") {
		// The access token is usable even if we cannot renew it early.
		return t.ExpiresAt != 0 && now.UnixMilli() >= t.ExpiresAt
	}
	return t.expired(now)
}

// accessToken serializes refreshes (including rotating refresh tokens). A 401
// refreshes the token that was rejected, not one another worker just obtained.
func (b *DropboxBackend) accessToken(ctx context.Context, rejected *DropboxToken) (*DropboxToken, error) {
	select {
	case b.tokenGate <- struct{}{}:
		defer func() { <-b.tokenGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.authErr != nil {
		return nil, b.authErr
	}
	t := b.token.Load()
	if b.pendingToken != nil {
		// A rotated refresh token may have invalidated the old one even
		// if the disk write failed. Retain it and retry saving, not OAuth.
		if err := b.persistToken(b.pendingToken); err != nil {
			return nil, err
		}
		t = b.pendingToken
		b.token.Store(t)
		b.pendingToken = nil
	}
	if t != rejected && !b.needsRefresh(t) {
		return t, nil
	}
	if t == nil || t.RefreshToken == "" {
		b.authErr = fmt.Errorf("no Dropbox refresh token: %w", ErrDropboxReauthorize)
		return nil, b.authErr
	}
	if b.oauth == nil || b.oauth.AppKey == "" {
		b.authErr = fmt.Errorf("refreshing this Dropbox token requires its original OAuth app key: %w", ErrDropboxReauthorize)
		return nil, b.authErr
	}
	next, err := b.oauth.Refresh(ctx, t.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrDropboxReauthorize) {
			b.authErr = err
		}
		return nil, fmt.Errorf("cannot refresh Dropbox token: %w", err)
	}
	if next.RefreshToken == "" {
		next.RefreshToken = t.RefreshToken
	}
	if next.AccountID == "" {
		next.AccountID = t.AccountID
	}
	if next.UID == "" {
		next.UID = t.UID
	}
	if next.Scope == "" {
		next.Scope = t.Scope
	}
	if err := b.persistToken(next); err != nil {
		b.pendingToken = next
		return nil, err
	}
	b.token.Store(next)
	return next, nil
}

func (b *DropboxBackend) persistToken(t *DropboxToken) error {
	if b.dataDir != "" {
		if err := SaveDropboxToken(b.dataDir, t); err != nil {
			return fmt.Errorf("cannot securely save refreshed Dropbox authorization: %w", err)
		}
	}
	return nil
}

func (b *DropboxBackend) rejectedToken(ctx context.Context, t *DropboxToken) error {
	select {
	case b.tokenGate <- struct{}{}:
		defer func() { <-b.tokenGate }()
		if b.token.Load() == t {
			b.authErr = ErrDropboxReauthorize
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return ErrDropboxReauthorize
}
