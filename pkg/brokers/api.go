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

package brokers

import (
	"context"
	raptorApi "github.com/raptor-ml/raptor/api/v1alpha1"
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
	Subscribe(context.Context, raptorApi.ParsedConfig) (context.Context, *pubsub.Subscription, error)
}

type ctxKey string

const dataSourceCtxKey ctxKey = "DataSource"

func ContextWithDataSource(ctx context.Context, dc *raptorApi.DataConnector) context.Context {
	return context.WithValue(ctx, dataSourceCtxKey, dc)
}
func DataSourceFromContext(ctx context.Context) *raptorApi.DataConnector {
	v := ctx.Value(dataSourceCtxKey)
	if v == nil {
		return nil
	}
	return v.(*raptorApi.DataSource)
}
