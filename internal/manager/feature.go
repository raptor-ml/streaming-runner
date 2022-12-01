/*
Copyright (c) 2022 RaptorML authors.

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
	"github.com/pkg/errors"
	"github.com/raptor-ml/raptor/api"
	raptorApi "github.com/raptor-ml/raptor/api/v1alpha1"
	"github.com/raptor-ml/raptor/pkg/protoregistry"
	"github.com/raptor-ml/streaming-runner/pkg/brokers"
	"gocloud.dev/pubsub"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
	"net/url"
	"strings"
)

type Feature struct {
	Schema   string   `json:"schema,omitempty"`
	Packages []string `json:"packages,omitempty"`
	*api.FeatureDescriptor
}

// if a particular feature extraction has failed, it should log it and allow other to live in peace
func (m *manager) getFeatureDefinitions(ctx context.Context, in *raptorApi.DataSource, bsc BaseStreaming) []*Feature {
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
func (m *manager) getFeature(ctx context.Context, ref raptorApi.ResourceReference, bs BaseStreaming) (*Feature, error) {
	ftSpec := raptorApi.Feature{}
	err := m.client.Get(ctx, ref.ObjectKey(), &ftSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch feature definition: %w", err)
	}

	ft := &Feature{}
	err = json.Unmarshal(ftSpec.Spec.Builder.Raw, ft)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal feature definition: %w", err)
	}

	if strings.ToLower(ftSpec.Spec.Builder.Kind) != "streaming" {
		return nil, fmt.Errorf("feature definition kind is not supported: %s", ftSpec.Spec.Builder.Kind)
	}

	ft.Packages = ftSpec.Spec.Builder.Packages

	if ft.Schema == "" && bs.Schema != nil {
		ft.Schema = bs.Schema.String()
	}
	if ft.Schema != "" {
		u, err := url.Parse(ft.Schema)
		if err == nil && u.Scheme != "" && u.Host != "" && u.Fragment != "" {
			if !(bs.Schema != nil && u.Scheme == bs.Schema.Scheme && u.Host == bs.Schema.Host) {
				_, err := protoregistry.Register(ft.Schema)
				if err != nil {
					return nil, fmt.Errorf("failed to register schema: %w", err)
				}
			}
		} else {
			return nil, fmt.Errorf("invalid schema provided (did you mentioned the message type?)")
		}
	}

	ft.FeatureDescriptor, err = api.FeatureDescriptorFromManifest(&ftSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to create feature descriptor: %w", err)
	}

	_, err = m.runtimeManager.LoadProgram(ft.RuntimeEnv, ft.FQN, ftSpec.Spec.Builder.Code, ft.Packages)
	return ft, err
}

func (m *manager) handle(ctx context.Context, msg *pubsub.Message, md brokers.Metadata, bs BaseStreaming) error {
	for _, ft := range bs.features {

		var jsonMsg []byte
		var row map[string]any
		if ft.Schema != "" {
			u, err := url.Parse(ft.Schema)
			if err != nil {
				return fmt.Errorf("failed to parse data schema: %w", err)
			}

			md, err := protoregistry.GetDescriptor(u.Fragment)
			if err != nil {
				if !errors.Is(err, protoregistry.ErrNotFound) {
					return fmt.Errorf("failed to find proto type for message")
				}

				pack, err := protoregistry.Register(ft.Schema)
				if err != nil && !errors.Is(err, protoregistry.ErrAlreadyRegistered) {
					return fmt.Errorf("failed to register proto type: %w", err)
				}

				s := u.Fragment
				if strings.Count(s, ".") < 1 {
					s = fmt.Sprintf("%s.%s", pack, u.Fragment)
				}
				md, err = protoregistry.GetDescriptor(s)
				if err != nil {
					panic(fmt.Errorf("failed to get a schema that was just registered: %w", err))
				}
			}
			pm := dynamicpb.NewMessage(md)
			err = proto.Unmarshal(msg.Body, pm)
			if err != nil {
				return fmt.Errorf("failed to parse message to proto: %w", err)
			}

			// marshal to the row map
			jsonMsg, err = protojson.Marshal(pm)
			if err != nil {
				return fmt.Errorf("failed to marshal proto to json: %w", err)
			}
		} else {
			jsonMsg = msg.Body
		}

		// if schema is not provided, we assume that the message is a json
		// now we need to unmarshal it to a map
		err := json.Unmarshal(jsonMsg, &row)
		if err != nil {
			return fmt.Errorf("failed to unmarshal message: %w", err)
		}
		row = flattenMap(row)

		keys := api.Keys{}
		for _, k := range ft.Keys {
			if _, ok := row[k]; !ok {
				return fmt.Errorf("key %s is missing in the message", k)
			}
			keys[k] = fmt.Sprintf("%s", row[k])
		}

		_, _, err = m.runtimeManager.ExecuteProgram(ctx, ft.RuntimeEnv, ft.FQN, keys, row, md.Timestamp, false)
		if err != nil {
			return fmt.Errorf("failed to execute feature: %w", err)
		}
	}
	return nil
}

func flattenMap(row map[string]any) map[string]any {
	//flatten maps
	ret := make(map[string]any)
	for k, v := range row {
		switch v := v.(type) {
		case map[string]any:
			for k1, v1 := range flattenMap(v) {
				ret[fmt.Sprintf("%s.%s", k, k1)] = v1
			}
		default:
			ret[k] = v
		}
	}
	return ret
}
