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

package brokers

import (
	"context"
	natunApi "github.com/natun-ai/natun/api/v1alpha1"
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
	Subscribe(context.Context, natunApi.ParsedConfig) (context.Context, *pubsub.Subscription, error)
}

type ctxKey string

const dataConnectorCtxKey ctxKey = "DataConnector"

func ContextWithDataConnector(ctx context.Context, dc *natunApi.DataConnector) context.Context {
	return context.WithValue(ctx, dataConnectorCtxKey, dc)
}
func DataConnectorFromContext(ctx context.Context) *natunApi.DataConnector {
	v := ctx.Value(dataConnectorCtxKey)
	if v == nil {
		return nil
	}
	return v.(*natunApi.DataConnector)
}
