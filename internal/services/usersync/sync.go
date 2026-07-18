package usersync

import (
	"context"
	"fmt"
	"time"

	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Service provisions users in auth-service (SSO) for a tenant. It calls auth-api's
// S2S member endpoint POST /api/v1/s2s/tenants/{tenant_id}/members (INTERNAL_SERVICE_KEY
// gated), which resolves-or-creates the account by email, upserts the tenant membership
// and publishes auth.user.created. (The previously-called /api/v1/admin/users/sync route
// never existed on auth-api — every sync 404'd.)
type Service struct {
	authServiceURL string
	apiKey         string
	serviceClient  *serviceclient.Client
	logger         *zap.Logger
}

// NewService creates a new user sync service. apiKey must be the shared
// INTERNAL_SERVICE_KEY (sent as X-API-Key).
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

// SyncUserRequest represents the request to provision a user with auth-service.
type SyncUserRequest struct {
	Email    string
	TenantID uuid.UUID
	// Roles for the tenant membership. Defaults to ["staff"]. NOTE: auth-api REPLACES
	// an existing membership's roles with this list — pass the user's real roles when
	// syncing someone who may already be a member.
	Roles   []string
	Service string
}

// SyncUserResponse represents the provisioning result.
type SyncUserResponse struct {
	UserID   uuid.UUID `json:"user_id"`
	Email    string    `json:"email"`
	TenantID uuid.UUID `json:"tenant_id"`
	Created  bool      `json:"created"`
}

// memberResponse mirrors auth-api's tenantMemberResponse (subset we need).
type memberResponse struct {
	UserID       string `json:"user_id"`
	TenantID     string `json:"tenant_id"`
	TempPassword string `json:"temp_password"`
}

// SyncUser provisions the user in auth-service and returns the auth user id.
func (s *Service) SyncUser(ctx context.Context, req SyncUserRequest) (*SyncUserResponse, error) {
	if s.apiKey == "" {
		s.logger.Warn("INTERNAL_SERVICE_KEY not configured, skipping user sync")
		return nil, fmt.Errorf("internal service key not configured")
	}
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("tenant id is required")
	}

	roles := req.Roles
	if len(roles) == 0 {
		roles = []string{"staff"}
	}
	service := req.Service
	if service == "" {
		service = "projects"
	}

	headers := map[string]string{
		"X-API-Key": s.apiKey,
	}
	body := map[string]any{
		"email":   req.Email,
		"roles":   roles,
		"service": service,
	}

	path := fmt.Sprintf("/api/v1/s2s/tenants/%s/members", req.TenantID)
	resp, err := s.serviceClient.Post(ctx, path, body, headers)
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

	var member memberResponse
	if err := resp.DecodeJSON(&member); err != nil {
		return nil, fmt.Errorf("decode sync response: %w", err)
	}
	userID, err := uuid.Parse(member.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user_id in sync response: %w", err)
	}
	tenantID, err := uuid.Parse(member.TenantID)
	if err != nil {
		tenantID = req.TenantID
	}

	syncResp := &SyncUserResponse{
		UserID:   userID,
		Email:    req.Email,
		TenantID: tenantID,
		// temp_password is only returned when a brand-new account was created.
		Created: member.TempPassword != "",
	}

	s.logger.Info("user synced with auth-service",
		zap.String("user_id", syncResp.UserID.String()),
		zap.String("email", syncResp.Email),
		zap.Bool("created", syncResp.Created),
	)

	return syncResp, nil
}
