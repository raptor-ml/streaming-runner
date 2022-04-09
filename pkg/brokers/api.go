package brokers

import (
	"context"
	"github.com/natun-ai/natun/pkg/api/v1alpha1"
	"gocloud.dev/pubsub"
	"time"
)

type Metadata struct {
	Topic     string
	Timestamp time.Time
	ID        string
}

type MetadataExtractor func(ctx context.Context, msg *pubsub.Message) Metadata
type Unmarshaler func(any) error

type Broker interface {
	Metadata(context.Context, *pubsub.Message) Metadata
	Subscribe(context.Context, v1alpha1.ParsedConfig) (context.Context, *pubsub.Subscription, error)
}
