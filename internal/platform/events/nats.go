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

	// One subject per the shared-events convention ({aggregate_type}.{event_type});
	// notifications-api and treasury bind project.>.
	if _, err := js.StreamInfo(cfg.StreamName); err == nil {
		return nil
	}

	_, err = js.AddStream(&nats.StreamConfig{
		Name:     cfg.StreamName,
		Subjects: []string{"project.>"},
		Replicas: 1,
	})
	return err
}
