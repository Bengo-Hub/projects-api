package events

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/bengobox/projects-service/internal/config"
)

func Connect(cfg config.EventsConfig) (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Name("projects-service"),
		nats.Timeout(5 * time.Second),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
	}

	return nats.Connect(cfg.NATSURL, opts...)
}

func EnsureStream(ctx context.Context, nc *nats.Conn, cfg config.EventsConfig) error {
	if nc == nil {
		return fmt.Errorf("nats connection is nil")
	}

	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("jetstream init: %w", err)
	}

	// Capture both the singular aggregate subject "project.>" (shared-events convention,
	// what notifications-api binds to) and the legacy "projects.>" prefix.
	subjects := []string{"project.>", "projects.>"}

	info, err := js.StreamInfo(cfg.StreamName)
	if err == nil {
		// Stream exists — ensure it captures project.> (older deployments only had projects.>).
		have := map[string]bool{}
		for _, s := range info.Config.Subjects {
			have[s] = true
		}
		missing := false
		for _, s := range subjects {
			if !have[s] {
				missing = true
			}
		}
		if missing {
			updated := info.Config
			updated.Subjects = subjects
			_, _ = js.UpdateStream(&updated)
		}
		return nil
	}

	_, err = js.AddStream(&nats.StreamConfig{
		Name:     cfg.StreamName,
		Subjects: subjects,
		Replicas: 1,
	})
	return err
}

