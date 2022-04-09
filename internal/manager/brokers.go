package manager

import (
	"context"
	"fmt"
	natunApi "github.com/natun-ai/natun/pkg/api/v1alpha1"
	"gocloud.dev/pubsub"
	"time"
)

func createSubscription(ctx context.Context, kind string, in *natunApi.DataConnector) (*pubsub.Subscription, error) {
	switch kind {
	case "kafka":
		return kafkaConsumer(ctx, in)
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}
func enrichMessage(kind string, msg *pubsub.Message) enrichedMetadata {
	em := enrichedMetadata{}
	switch kind {
	case "kafka":
		em = enrichKafkaMessage(msg)
	}

	if em.Timestamp.IsZero() {
		em.Timestamp = time.Now()
	}

	return em
}

type enrichedMetadata struct {
	Topic     string
	Timestamp time.Time
}
