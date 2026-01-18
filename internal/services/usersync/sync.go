package usersync

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"go.uber.org/zap"
)

// Service handles user synchronization with auth-service SSO
type Service struct {
	authServiceURL string
	apiKey         string
	serviceClient  *serviceclient.Client
	logger         *zap.Logger
}

// NewService creates a new user sync service
func NewService(authServiceURL, apiKey string, logger *zap.Logger) *Service {
	cfg := serviceclient.DefaultConfig(
		authServiceURL,
		"projects-service",
		logger.Named("usersync"),
	)
	cfg.Timeout = 10 * time.Second

	return &Service{
		authServiceURL: authServiceURL,
		apiKey:         apiKey,
		serviceClient:  serviceclient.New(cfg),
		logger:         logger,
	}
}

// SyncUserRequest represents the request to sync a user with auth-service
type SyncUserRequest struct {
	Email      string                 `json:"email"`
	Password   string                 `json:"password,omitempty"`
	TenantSlug string                 `json:"tenant_slug"`
	Profile    map[string]interface{} `json:"profile,omitempty"`
	Service    string                 `json:"service,omitempty"`
}

// SyncUserResponse represents the response from auth-service
type SyncUserResponse struct {
	UserID   uuid.UUID `json:"user_id"`
	Email    string    `json:"email"`
	TenantID uuid.UUID `json:"tenant_id"`
	Created  bool      `json:"created"`
	Message  string    `json:"message"`
}

// SyncUser syncs a user with auth-service SSO
func (s *Service) SyncUser(ctx context.Context, req SyncUserRequest) (*SyncUserResponse, error) {
	if s.apiKey == "" {
		s.logger.Warn("auth-service API key not configured, skipping user sync")
		return nil, fmt.Errorf("auth-service API key not configured")
	}

	headers := map[string]string{
		"X-API-Key": s.apiKey,
	}

	resp, err := s.serviceClient.Post(ctx, "/api/v1/admin/users/sync", req, headers)
	if err != nil {
		return nil, fmt.Errorf("sync user request failed: %w", err)
	}

	if !resp.IsSuccess() {
		var errResp map[string]interface{}
		_ = resp.DecodeJSON(&errResp)
		s.logger.Warn("user sync failed",
			zap.Int("status", resp.StatusCode),
			zap.Any("error", errResp),
			zap.String("email", req.Email),
		)
		return nil, fmt.Errorf("user sync failed: status %d", resp.StatusCode)
	}

	var syncResp SyncUserResponse
	if err := resp.DecodeJSON(&syncResp); err != nil {
		return nil, fmt.Errorf("decode sync response: %w", err)
	}

	s.logger.Info("user synced with auth-service",
		zap.String("user_id", syncResp.UserID.String()),
		zap.String("email", syncResp.Email),
		zap.Bool("created", syncResp.Created),
	)

	return &syncResp, nil
}
