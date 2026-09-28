package handlers

import (
	"net/http"

	"github.com/google/uuid"

	authclient "github.com/Bengo-Hub/shared-auth-client"
)

// actorID is the signed-in user, or uuid.Nil for a service call.
func actorID(r *http.Request) uuid.UUID {
	if claims, ok := authclient.ClaimsFromContext(r.Context()); ok {
		if uid, err := uuid.Parse(claims.Subject); err == nil {
			return uid
		}
	}
	return uuid.Nil
}

func uuidPtr(id uuid.UUID) *uuid.UUID { return &id }
