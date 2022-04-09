package manager

import (
	octrace "go.opencensus.io/trace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/bridge/opencensus"
)

func init() {
	tracer := otel.GetTracerProvider().Tracer("manager")
	octrace.DefaultTracer = opencensus.NewTracer(tracer)
}
