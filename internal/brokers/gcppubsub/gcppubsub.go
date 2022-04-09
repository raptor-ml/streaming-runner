package gcppubsub

import (
	"context"
	"fmt"
	"github.com/natun-ai/natun/pkg/api/v1alpha1"
	"github.com/natun-ai/streaming-runner/pkg/brokers"
	"gocloud.dev/gcp"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/gcppubsub"
	"golang.org/x/oauth2/google"
	pb "google.golang.org/genproto/googleapis/pubsub/v1"
)

func init() {
	brokers.Register("gcp_pubsub", &provider{})
}

type provider struct{}

const TopicContextKey = "topic"
const ProjectIDContextKey = "project_id"

func (p *provider) Metadata(ctx context.Context, msg *pubsub.Message) brokers.Metadata {
	var md brokers.Metadata
	var m *pb.PubsubMessage
	if ok := msg.As(&m); ok {
		md.Timestamp = m.GetPublishTime().AsTime()
		md.ID = m.GetMessageId()
		md.Topic = ctx.Value(TopicContextKey).(string)
	}
	return md
}

type config struct {
	ProjectID      string `mapstructure:"project_id"`
	Topic          string
	CredentialJSON []byte `mapstructure:"credential_json,omitempty"`
	MaxBatchSize   int
}

func (p *provider) Subscribe(ctx context.Context, c v1alpha1.ParsedConfig) (context.Context, *pubsub.Subscription, error) {
	cfg := config{}
	err := c.Unmarshal(&cfg)
	if err != nil {
		return ctx, nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	ctx = context.WithValue(context.WithValue(ctx, TopicContextKey, cfg.Topic), ProjectIDContextKey, cfg.ProjectID)

	var creds *google.Credentials
	if cfg.CredentialJSON != nil {
		creds, err = google.CredentialsFromJSON(ctx, cfg.CredentialJSON, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return ctx, nil, fmt.Errorf("failed to parse credential json: %w", err)
		}
	} else {
		creds, err = gcp.DefaultCredentials(ctx)
		if err != nil {
			return ctx, nil, err
		}
	}

	// Open a gRPC connection to the GCP Pub/Sub API.
	conn, cleanup, err := gcppubsub.Dial(ctx, creds.TokenSource)
	if err != nil {
		return ctx, nil, err
	}
	go func() {
		<-ctx.Done()
		cleanup()
	}()

	// Construct a SubscriberClient using the connection.
	subClient, err := gcppubsub.SubscriberClient(ctx, conn)
	if err != nil {
		return ctx, nil, err
	}
	go func() {
		<-ctx.Done()
		_ = subClient.Close()
	}()

	sub, err := gcppubsub.OpenSubscriptionByPath(subClient,
		fmt.Sprintf("projects/%s/subscriptions/%s", cfg.ProjectID, cfg.Topic),
		&gcppubsub.SubscriptionOptions{MaxBatchSize: cfg.MaxBatchSize},
	)
	return ctx, sub, err
}
