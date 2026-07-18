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

	// Singular aggregate subject only (shared-events convention: subject =
	// {aggregate_type}.{event_type}; notifications-api binds project.>). The legacy
	// "projects.>" binding was dropped — nothing ever published or consumed it.
	subjects := []string{"project.>"}

	info, err := js.StreamInfo(cfg.StreamName)
	if err == nil {
		// Stream exists — reconcile the subject set (drops legacy projects.>, adds project.>
		// on older deployments).
		if len(info.Config.Subjects) != len(subjects) || info.Config.Subjects[0] != subjects[0] {
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

