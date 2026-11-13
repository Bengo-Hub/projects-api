package qa

import (
	"context"
	"encoding/json"
	"time"

	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	featureChangedSubject = "subscriptions.feature.changed"
	featureChangedDurable = "projects-qa-feature-changed"
)

// FeatureEventsConsumer flags templates for review when a catalogue feature their criteria check
// changes (subscriptions.feature.changed, published by subscriptions-api on every catalogue
// upsert). Setting the flag is idempotent, so a redelivered event is harmless.
type FeatureEventsConsumer struct {
	svc *Service
	log *zap.Logger
}

func NewFeatureEventsConsumer(svc *Service, log *zap.Logger) *FeatureEventsConsumer {
	return &FeatureEventsConsumer{svc: svc, log: log.Named("qa.feature_events")}
}

// Start waits for the stream subscriptions-api publishes on (it owns it) and consumes until ctx
// ends. Until subscriptions-api publishes the event, this simply keeps waiting.
func (c *FeatureEventsConsumer) Start(ctx context.Context, nc *nats.Conn) error {
	if nc == nil {
		c.log.Warn("NATS not available; feature change consumer disabled")
		return nil
	}
	js, err := nc.JetStream()
	if err != nil {
		return err
	}
	stream := ""
	for backoff := 5 * time.Second; stream == ""; backoff = min(backoff*2, 5*time.Minute) {
		if name, err := js.StreamNameBySubject(featureChangedSubject); err == nil && name != "" {
			stream = name
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
	}
	eventslib.SubscribeQueueWithRebind(c.log, js, stream, featureChangedSubject, featureChangedDurable, c.handle,
		nats.Durable(featureChangedDurable), nats.AckExplicit(), nats.AckWait(30*time.Second), nats.MaxDeliver(5), nats.DeliverAll())
	<-ctx.Done()
	return nil
}

// decodeFeatureChanged reads the feature code from the event envelope. The documented payload
// carries code, service, module and change kind; feature_code is accepted too.
func decodeFeatureChanged(data []byte) string {
	var env struct {
		Payload struct {
			Code        string `json:"code"`
			FeatureCode string `json:"feature_code"`
		} `json:"payload"`
	}
	if json.Unmarshal(data, &env) != nil {
		return ""
	}
	if env.Payload.Code != "" {
		return env.Payload.Code
	}
	return env.Payload.FeatureCode
}

func (c *FeatureEventsConsumer) handle(msg *nats.Msg) {
	code := decodeFeatureChanged(msg.Data)
	if code == "" {
		_ = msg.Ack()
		return
	}
	n, err := c.svc.MarkFeatureChanged(context.Background(), code)
	if err != nil {
		c.log.Warn("templates not flagged for review; will retry", zap.String("feature", code), zap.Error(err))
		_ = msg.Nak()
		return
	}
	if n > 0 {
		c.log.Info("templates flagged for review", zap.String("feature", code), zap.Int("templates", n))
	}
	_ = msg.Ack()
}
