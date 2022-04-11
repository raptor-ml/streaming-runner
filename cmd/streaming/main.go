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

package main

import (
	"context"
	"fmt"
	"github.com/go-logr/zapr"
	grpcMiddleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpcZap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpcRetry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"github.com/natun-ai/streaming-runner/internal/manager"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	pbRuntime "go.buf.build/natun/api-go/natun/runtime/natun/runtime/v1alpha1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"os"
	"os/signal"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"syscall"
)

func main() {
	pflag.Bool("production", true, "Set as production")
	pflag.String("dataconnector-resource", "", "The resource name of the DataConnector")
	pflag.String("dataconnector-namespace", "", "The namespace name of the DataConnector")
	pflag.String("runtime-grpc-url", "http://localhost:70001", "The gRPC URL of the Natun Runtime")
	pflag.Parse()
	must(viper.BindPFlags(pflag.CommandLine))

	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	zl := logger()
	logger := zapr.NewLogger(zl)

	if viper.GetString("dataconnector-resource") == "" || viper.GetString("dataconnector-namespace") == "" {
		logger.Error(fmt.Errorf("missing required configuration"), "`dataconnector-resource` and `dataconnector-namespace` are required")
		os.Exit(30)
	}

	cc, err := grpc.Dial(
		viper.GetString("runtime-grpc-url"),
		grpc.WithStreamInterceptor(grpcMiddleware.ChainStreamClient(
			grpcZap.StreamClientInterceptor(zl.Named("runtime")),
			grpcRetry.StreamClientInterceptor(),
		)),
		grpc.WithUnaryInterceptor(grpcMiddleware.ChainUnaryClient(
			grpcZap.UnaryClientInterceptor(zl.Named("runtime")),
			grpcRetry.UnaryClientInterceptor(),
		)),
	)
	must(err)
	runtime := pbRuntime.NewRuntimeServiceClient(cc)

	conn := client.ObjectKey{
		Name:      viper.GetString("dataconnector-resource"),
		Namespace: viper.GetString("dataconnector-namespace"),
	}
	mgr, err := manager.New(conn, runtime, ctrl.GetConfigOrDie(), logger.WithName("manager"))
	must(err)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err = mgr.Start(ctx)
	must(err)
	defer cancel()

}
func logger() *zap.Logger {
	var l *zap.Logger
	var err error
	if viper.GetBool("production") {
		l, err = zap.NewProduction()
	} else {
		l, err = zap.NewDevelopment()
	}
	must(err)

	return l
}

func must(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
