package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-logr/logr"
	natunApi "github.com/natun-ai/natun/pkg/api/v1alpha1"
	"github.com/natun-ai/natun/pkg/pyexp"
	"github.com/natun-ai/streaming-runner/pkg/brokers"
	"github.com/natun-ai/streaming-runner/pkg/protoregistry"
	"go.starlark.net/lib/proto"
	"go.starlark.net/starlark"
	"gocloud.dev/pubsub"
	"net/url"
	"strings"
)

type Feature struct {
	natunApi.FeatureBuilderType `json:",inline"`
	FQN                         string `json:"-"`
	Schema                      string `json:"schema,omitempty"`
	Expression                  string `json:"expression"`
	runtime                     pyexp.Runtime
}

// if a particular feature extraction has failed, it should log it and allow other to live in peace
func (m *manager) getFeatureDefinitions(ctx context.Context, in *natunApi.DataConnector, bsc BaseStreaming) []*Feature {
	var features []*Feature
	m.logger.Info("fetching feature definitions...")
	for _, ref := range in.Status.Features {
		m.logger.V(1).Info(fmt.Sprintf("fetching feature definition: %s", ref.Name))

		ft, err := m.getFeature(ctx, ref, bsc)
		if err != nil {
			m.logger.Error(err, "failed to fetch feature")
		}
		features = append(features, ft)
	}
	return features
}
func (m *manager) getFeature(ctx context.Context, ref natunApi.ResourceReference, bs BaseStreaming) (*Feature, error) {
	ftSpec := natunApi.Feature{}
	err := m.client.Get(ctx, ref.ObjectKey(), &ftSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch feature definition: %w", err)
	}

	ft := &Feature{}
	err = json.Unmarshal(ftSpec.Spec.Builder.Raw, ft)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal feature definition: %w", err)
	}

	if ft.FeatureBuilderType.Kind != "streaming" {
		return nil, fmt.Errorf("feature definition kind is not supported: %s", ft.FeatureBuilderType.Kind)
	}
	ft.FQN = ftSpec.FQN()

	if ft.Schema != "" {
		u, err := url.Parse(ft.Schema)
		if err == nil && u.Scheme != "" && u.Host != "" && u.Fragment != "" {
			pack, err := protoregistry.Register(ft.Schema)
			if err != nil && !errors.Is(err, protoregistry.ErrAlreadyRegistered) {
				return nil, fmt.Errorf("failed to register proto schema: %w", err)
			}
			ft.Schema = u.Fragment
			if strings.Count(ft.Schema, ".") < 1 {
				ft.Schema = fmt.Sprintf("%s.%s", pack, ft.Schema)
			}
		} else {
			if bs.schemaPack != "" && strings.Count(ft.Schema, ".") < 1 {
				ft.Schema = fmt.Sprintf("%s.%s", bs.schemaPack, ft.Schema)
			}
		}
	}

	ft.runtime, err = pyexp.New(ft.FQN, ft.Expression, m.engine)
	if err != nil {
		return nil, fmt.Errorf("failed to create feature runtime: %w", err)
	}

	return ft, nil
}

func (m *manager) handle(ctx context.Context, msg *pubsub.Message, md brokers.Metadata, bs BaseStreaming) error {
	for _, ft := range bs.features {
		var payload starlark.Value
		schema := ""
		if ft.Schema != "" {
			schema = ft.Schema
		} else if bs.schemaMsg != "" {
			schema = bs.schemaMsg
		}
		if schema != "" {
			md, err := protoregistry.GetDescriptor(ft.Schema)
			if err != nil {
				return fmt.Errorf("failed to find proto type for message")
			}

			payload, err = proto.Unmarshal(md, msg.Body)
			if err != nil {
				return fmt.Errorf("failed to parse message to proto")
			}
		} else {
			payload = starlark.String(msg.Body)
		}

		headers := fixHeaders(msg.Metadata)
		headers["X-NATUN-STREAMING-TOPIC"] = []string{md.Topic}
		headers["X-NATUN-STREAMING-ID"] = []string{md.ID}

		val, ts, eid, err := ft.runtime.Exec(ctx, pyexp.ExecRequest{
			Headers:   headers,
			Payload:   payload,
			Fqn:       ft.FQN,
			Timestamp: md.Timestamp,
			Logger:    logr.Logger{},
		})

		if err != nil {
			m.logger.Error(err, "failed to execute feature")
			continue
		}
		if val != nil {
			if eid == "" {
				m.logger.Error(fmt.Errorf("feature did not return an entity id"), "failed to execute feature")
				continue
			}
			err := m.engine.Update(ctx, ft.FQN, eid, val, ts)
			if err != nil {
				m.logger.Error(err, "failed to update feature")
				continue
			}
		}
	}
	return nil
}

func fixHeaders(metadata map[string]string) map[string][]string {
	headers := make(map[string][]string)
	for k, v := range metadata {
		headers[k] = []string{v}
	}
	return headers
}
