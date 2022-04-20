/*
Copyright 2022 Natun.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/Shopify/sarama"
	"github.com/natun-ai/natun/pkg/api/v1alpha1"
	"github.com/natun-ai/streaming-runner/pkg/brokers"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/kafkapubsub"
	"strconv"
	"strings"
)

func init() {
	brokers.Register("kafka", &provider{})
}

type provider struct{}

func (p *provider) Metadata(_ context.Context, msg *pubsub.Message) brokers.Metadata {
	var md brokers.Metadata
	var m *sarama.ConsumerMessage
	if ok := msg.As(&m); ok {
		md.Timestamp = m.Timestamp
		md.Topic = m.Topic
		md.ID = strconv.FormatInt(m.Offset, 10)
	}
	return md
}

type config struct {
	Brokers       []string
	Topics        []string
	ConsumerGroup string
	ClientID      string

	SaslUsername string
	SaslPassword string

	TLSDisable    bool
	TLSSkipVerify bool
	TLSCaCert     string
	TLSClientCert string
	TLSClientKey  string

	InitialOffset string
	Version       string
}

func (p *provider) Subscribe(ctx context.Context, c v1alpha1.ParsedConfig) (context.Context, *pubsub.Subscription, error) {
	cfg := config{}
	err := c.Unmarshal(&cfg)
	if err != nil {
		return ctx, nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if len(cfg.Brokers) == 0 {
		return ctx, nil, fmt.Errorf("brokers required to connect to kafka")
	}
	if len(cfg.Topics) == 0 {
		return ctx, nil, fmt.Errorf("topics required to connect to kafka")
	}

	if cfg.ConsumerGroup == "" {
		dc := brokers.DataConnectorFromContext(ctx)
		if dc == nil {
			panic("no DataConnector in context")
		}
		cfg.ConsumerGroup = fmt.Sprintf("%s.%s", dc.Name, dc.Namespace)
	}

	// The Kafka client configuration to use.
	config := kafkapubsub.MinimalConfig()

	if cfg.Version != "" {
		if ver, err := sarama.ParseKafkaVersion(cfg.Version); err == nil {
			if ver.IsAtLeast(config.Version) {
				return ctx, nil, fmt.Errorf("kafka version %s is not supported", cfg.Version)
			}
			config.Version = ver
		} else {
			return ctx, nil, fmt.Errorf("failed to parse kafka version: %w", err)
		}
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
		return ctx, nil, err
	}

	if cfg.SaslUsername != "" && cfg.SaslPassword != "" {
		config.Net.SASL.Handshake = true
		config.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		config.Net.SASL.User = cfg.SaslUsername
		config.Net.SASL.Password = cfg.SaslPassword
	}

	sub, err := kafkapubsub.OpenSubscription(cfg.Brokers, config, cfg.ConsumerGroup, cfg.Topics, &kafkapubsub.SubscriptionOptions{
		KeyName: "key",
	})
	return ctx, sub, err
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

func updateTLSConfig(config *sarama.Config, in config) error {
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
