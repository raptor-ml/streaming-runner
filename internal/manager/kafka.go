package manager

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"github.com/Shopify/sarama"
	"github.com/natun-ai/natun/pkg/api/v1alpha1"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/kafkapubsub"
	"strings"
)

func enrichKafkaMessage(msg *pubsub.Message) enrichedMetadata {
	var em enrichedMetadata
	var m *sarama.ConsumerMessage
	if ok := msg.As(&m); ok {
		em.Timestamp = m.Timestamp
		em.Topic = m.Topic
	}
	return em
}

type kafkaConfig struct {
	Brokers       []string `json:"brokers,omitempty"`
	Topics        []string `json:"topics,omitempty"`
	ConsumerGroup string   `json:"consumer_group,omitempty"`
	ClientID      string   `json:"client_id,omitempty"`

	SaslUsername string `json:"sasl_username,omitempty"`
	SaslPassword string `json:"sasl_password,omitempty"`

	TLSDisable    bool   `json:"tls_disable,omitempty"`
	TLSSkipVerify bool   `json:"tls_skip_verify,omitempty"`
	TLSCaCert     string `json:"tls_ca_cert,omitempty"`
	TLSClientCert string `json:"tls_client_cert,omitempty"`
	TLSClientKey  string `json:"tls_client_key,omitempty"`

	InitialOffset string `json:"initial_offset,omitempty"`
	Version       string `json:"version,omitempty"`
}

func kafkaConsumer(ctx context.Context, in *v1alpha1.DataConnector) (*pubsub.Subscription, error) {
	cfg := kafkaConfig{}
	err := json.Unmarshal(in.Spec.Config.Raw, &cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("brokers required to connect to kafka")
	}
	if len(cfg.Topics) == 0 {
		return nil, fmt.Errorf("topics required to connect to kafka")
	}

	if cfg.ConsumerGroup == "" {
		cfg.ConsumerGroup = fmt.Sprintf("%s/%s", in.GetNamespace(), in.GetName())
	}

	// The Kafka client configuration to use.
	config := kafkapubsub.MinimalConfig()

	if ver, err := sarama.ParseKafkaVersion(cfg.Version); err == nil {
		if ver.IsAtLeast(config.Version) {
			return nil, fmt.Errorf("kafka version %s is not supported", cfg.Version)
		}
		config.Version = ver
	} else {
		return nil, fmt.Errorf("failed to parse kafka version: %w", err)
	}

	if io, err := parseInitialOffset(cfg.InitialOffset); err != nil {
		config.Consumer.Offsets.Initial = io
	}

	cfg.ClientID = "consumer.k8s.natun.ai"
	if cfg.ClientID != "" {
		config.ClientID = cfg.ClientID
	}

	err = updateTLSConfig(config, cfg)
	if err != nil {
		return nil, err
	}

	if cfg.SaslUsername != "" && cfg.SaslPassword != "" {
		config.Net.SASL.Handshake = true
		config.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		config.Net.SASL.User = cfg.SaslUsername
		config.Net.SASL.Password = cfg.SaslPassword
	}

	return kafkapubsub.OpenSubscription(cfg.Brokers, config, cfg.ConsumerGroup, cfg.Topics, &kafkapubsub.SubscriptionOptions{
		KeyName: "key",
	})
}

func parseInitialOffset(value string) (initialOffset int64, err error) {
	initialOffset = sarama.OffsetNewest // Default
	if strings.EqualFold(value, "oldest") {
		initialOffset = sarama.OffsetOldest
	} else if strings.EqualFold(value, "newest") {
		initialOffset = sarama.OffsetNewest
	} else if value != "" {
		return 0, fmt.Errorf("kafka error: invalid initialOffset: %s", value)
	}

	return initialOffset, err
}

func updateTLSConfig(config *sarama.Config, in kafkaConfig) error {
	if in.TLSDisable {
		config.Net.TLS.Enable = false
		return nil
	}
	config.Net.TLS.Enable = true

	if !in.TLSSkipVerify && in.TLSCaCert == "" {
		return nil
	}

	config.Net.TLS.Config = &tls.Config{InsecureSkipVerify: in.TLSSkipVerify, MinVersion: tls.VersionTLS12}
	if in.TLSCaCert != "" {
		caCertPool := x509.NewCertPool()
		if ok := caCertPool.AppendCertsFromPEM([]byte(in.TLSCaCert)); !ok {
			return fmt.Errorf("kafka error: unable to load ca certificate")
		}
		config.Net.TLS.Config.RootCAs = caCertPool
	}

	return nil
}
