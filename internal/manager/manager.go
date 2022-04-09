package manager

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/natun-ai/natun/pkg/api"
	natunApi "github.com/natun-ai/natun/pkg/api/v1alpha1"
	"github.com/natun-ai/streaming-runner/pkg/brokers"
	"github.com/natun-ai/streaming-runner/pkg/protoregistry"
	"gocloud.dev/pubsub"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"net/url"
	ctrlCache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

type Manager interface {
	Start(ctx context.Context) error
}
type manager struct {
	client     ctrlCache.Cache
	logger     logr.Logger
	cancel     context.CancelFunc
	engine     api.Engine
	brokerType string
	conn       client.ObjectKey
}

func New(conn client.ObjectKey, engine api.Engine, cfg *rest.Config, logger logr.Logger) (Manager, error) {
	c, err := ctrlCache.New(cfg, ctrlCache.Options{
		Namespace: conn.Namespace,
		DefaultSelector: ctrlCache.ObjectSelector{
			Field: fields.OneTermEqualSelector("metadata.name", conn.Name),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create controler cache client: %w", err)
	}

	return &manager{
		client: c,
		logger: logger,
		engine: engine,
	}, nil
}

func (m *manager) Start(ctx context.Context) error {
	m.logger.Info("Starting...")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	i, err := m.client.GetInformer(ctx, &natunApi.DataConnector{})
	if err != nil {
		panic(err)
	}

	i.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.Add(ctx, obj.(*natunApi.DataConnector))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.Update(ctx, oldObj.(*natunApi.DataConnector), newObj.(*natunApi.DataConnector))
		},
		DeleteFunc: func(obj interface{}) {
			m.logger.Info("DataConnector deleted. Gracefully closing...")
			cancel()
		},
	})
	go func() {
		<-ctx.Done()
		if m.cancel != nil {
			m.cancel()
		}
	}()

	return m.client.Start(ctx)
}

type BaseStreaming struct {
	BrokerKind string
	Workers    int
	Schema     string

	schemaPack   string
	schemaMsg    string
	subscription *pubsub.Subscription
	mdExtractor  brokers.MetadataExtractor
	features     []*Feature
}

func (m *manager) Add(ctx context.Context, in *natunApi.DataConnector) {
	if in.Spec.Kind != "streaming" {
		m.logger.Error(fmt.Errorf("unsupported DataConenctor kind: %s", in.Spec.Kind), "kind is not streaming")
		return
	}

	cfg, err := in.ParseConfig(ctx, m.client)
	if err != nil {
		m.logger.Error(err, "failed to retrieve config")
	}

	bs := BaseStreaming{}
	err = cfg.Unmarshal(&bs)
	if err != nil {
		m.logger.Error(err, "failed to unmarshal streaming config")
		return
	}
	if bs.Workers == 0 {
		bs.Workers = 1
	}
	bs.BrokerKind = in.Spec.Kind

	if bs.Schema != "" {
		u, err := url.Parse(bs.Schema)
		if err == nil && u.Scheme != "" && u.Host != "" {
			pack, err := protoregistry.Register(bs.Schema)
			if err != nil && !errors.Is(err, protoregistry.ErrAlreadyRegistered) {
				m.logger.Error(err, "failed to register proto schema")
			}
			if u.Fragment != "" {
				if strings.Count(u.Fragment, ".") < 1 {
					bs.schemaMsg = fmt.Sprintf("%s.%s", pack, u.Fragment)
				} else {
					bs.schemaMsg = u.Fragment
				}
			}
			bs.schemaPack = pack
		}
	}

	broker := brokers.Get(bs.BrokerKind)
	if broker == nil {
		m.logger.Error(fmt.Errorf("broker %s not found", bs.BrokerKind), "invalid broker kind")
		return
	}
	bs.mdExtractor = broker.Metadata

	// Spawn a sub context for the broker
	// This allowing us to replace the broker context with a new one using cancel
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	// Create a new subscription
	ctx, bs.subscription, err = broker.Subscribe(ctx, cfg)
	if err != nil {
		m.logger.Error(err, "failed to create subscription")
		return
	}
	go func(ctx context.Context) {
		<-ctx.Done()
		err := bs.subscription.Shutdown(context.TODO())
		if err != nil {
			m.logger.Error(err, "failed to shutdown streaming")
		}
		m.cancel = nil
	}(ctx)

	bs.features = m.getFeatureDefinitions(ctx, in, bs)
	m.subscribe(ctx, bs)
}

func (m *manager) Update(ctx context.Context, _ *natunApi.DataConnector, in *natunApi.DataConnector) {
	if m.cancel != nil {
		m.cancel()
	}

	m.Add(ctx, in)
}

func (m *manager) subscribe(ctx context.Context, bs BaseStreaming) {
	for i := 0; i < bs.Workers; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					msg, err := bs.subscription.Receive(ctx)
					if err != nil {
						m.logger.Error(err, "failed to receive message")
						return
					}
					md := bs.mdExtractor(ctx, msg)
					if err := m.handle(ctx, msg, md, bs); err != nil {
						if msg.Nackable() {
							msg.Nack()
						}
						m.logger.Error(err, "failed to handle message")
					}

					msg.Ack()
				}
			}
		}()
	}
}
