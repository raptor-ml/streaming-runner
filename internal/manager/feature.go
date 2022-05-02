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

package manager

import (
	"context"
	"encoding/json"
	"fmt"
	ceProto "github.com/cloudevents/sdk-go/binding/format/protobuf/v2"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	natunApi "github.com/natun-ai/natun/api/v1alpha1"
	"github.com/natun-ai/streaming-runner/pkg/brokers"
	pbRuntime "go.buf.build/natun/api-go/natun/core/natun/runtime/v1alpha1"
	"gocloud.dev/pubsub"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"net/url"
	"strings"
)

type Feature struct {
	natunApi.FeatureBuilderKind `json:",inline"`
	Schema                      string `json:"schema,omitempty"`
	Expression                  string `json:"pyexp"`
	fqn                         string
	programHash                 string
}

func (m *manager) registerSchema(ctx context.Context, schema string) error {
	uuid := newUUID()
	resp, err := m.runtime.RegisterSchema(ctx, &pbRuntime.RegisterSchemaRequest{
		Uuid:   uuid,
		Schema: schema,
	})
	if err != nil {
		return fmt.Errorf("failed to register schema: %w", err)
	}
	if resp.GetUuid() != uuid {
		return fmt.Errorf("failed to register schema: unexpected uuid")
	}
	return nil
}
func (m *manager) registerProgram(ctx context.Context, ft *Feature) error {
	uuid := newUUID()
	resp, err := m.runtime.LoadPyExpProgram(ctx, &pbRuntime.LoadPyExpProgramRequest{
		Uuid:    uuid,
		Program: ft.Expression,
	})

	if err != nil {
		return fmt.Errorf("failed to register program: %w", err)
	}
	if resp.GetUuid() != uuid {
		return fmt.Errorf("failed to register program: unexpected uuid")
	}
	ft.programHash = resp.GetProgramHash()
	return nil
}

// if a particular feature extraction has failed, it should log it and allow other to live in peace
func (m *manager) getFeatureDefinitions(ctx context.Context, in *natunApi.DataConnector, bsc BaseStreaming) []*Feature {
	var features []*Feature
	m.logger.Info("fetching feature definitions...")
	for _, ref := range in.Status.Features {
		m.logger.V(1).Info(fmt.Sprintf("fetching feature definition: %s", ref.Name))

		// fix connector namespace
		if ref.Namespace == "" {
			ref.Namespace = in.Namespace
		}
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

	if ft.FeatureBuilderKind.Kind != "streaming" {
		return nil, fmt.Errorf("feature definition kind is not supported: %s", ft.FeatureBuilderKind.Kind)
	}
	ft.fqn = ftSpec.FQN()

	if ft.Schema != "" {
		u, err := url.Parse(ft.Schema)
		if err == nil && u.Scheme != "" && u.Host != "" && u.Fragment != "" {
			if !(u.Scheme == bs.Schema.Scheme && u.Host == bs.Schema.Host) {
				err := m.registerSchema(ctx, ft.Schema)
				if err != nil {
					return nil, fmt.Errorf("failed to register schema: %w", err)
				}
			}
		} else {
			u := &url.URL{}
			*u = *bs.Schema
			u.Fragment = ft.Schema
			ft.Schema = u.String()
		}
	} else {
		ft.Schema = bs.Schema.String()
	}

	return ft, m.registerProgram(ctx, ft)
}

func (m *manager) handle(ctx context.Context, msg *pubsub.Message, md brokers.Metadata, bs BaseStreaming) error {
	for _, ft := range bs.features {
		ev := cloudevents.NewEvent()
		ev.SetID(md.ID)
		ev.SetSource(m.conn.String())
		ev.SetTime(md.Timestamp)
		ev.SetDataSchema(ft.Schema)
		ev.SetSubject(md.Topic)

		contentType := ""
		u := url.URL{}
		for k, v := range msg.Metadata {
			if strings.ToLower(k) == "content-type" {
				contentType = v
			}
			u.Query().Add(k, v)
		}
		// Encode the parameters.
		u.RawQuery = u.Query().Encode()
		ev.SetExtension("headers", u)

		err := ev.SetData(contentType, msg.Body)
		if err != nil {
			return fmt.Errorf("failed to set data: %w", err)
		}

		pb, err := ceProto.ToProto(&ev)
		if err != nil {
			return fmt.Errorf("failed to convert event to protobuf: %w", err)
		}
		data, err := anypb.New(pb)
		if err != nil {
			return fmt.Errorf("failed to create anypb: %w", err)
		}

		req := &pbRuntime.ExecutePyExpRequest{
			Uuid:        newUUID(),
			Fqn:         ft.fqn,
			ProgramHash: ft.programHash,
			EntityId:    nil,
			Data:        data,
		}
		tries := 1
	exec:
		resp, err := m.runtime.ExecutePyExp(ctx, req)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				err := m.registerProgram(ctx, ft)
				if err != nil {
					return fmt.Errorf("failed to register program: %w", err)
				}
				if tries < 3 {
					tries++
					goto exec
				}
			}
			return fmt.Errorf("failed to execute program: %w", err)
		}
		if resp.GetUuid() != req.GetUuid() {
			return fmt.Errorf("failed to execute program: unexpected uuid")
		}
	}
	return nil
}
