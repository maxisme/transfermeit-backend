package tracer

import (
	"fmt"
	"go.opentelemetry.io/otel/api/global"
	"go.opentelemetry.io/otel/api/kv"
	"go.opentelemetry.io/otel/api/propagation"
	"go.opentelemetry.io/otel/api/trace"
	"go.opentelemetry.io/otel/exporters/trace/jaeger"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"log"
	"net/http"
	"os"
)

func Middleware(next http.Handler) http.Handler {
	props := propagation.New(propagation.WithExtractors(trace.B3{}))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := propagation.ExtractHTTP(r.Context(), props, r.Header)
		spanCtx := trace.RemoteSpanContextFromContext(ctx)
		ctx = trace.ContextWithRemoteSpanContext(ctx, spanCtx)
		tr := global.Tracer("")
		_, span := tr.Start(ctx, fmt.Sprintf("%s request", r.Method))
		next.ServeHTTP(w, r)
		span.End()
	})
}

func Init(serviceName, colectorHostname string) func() {
	// Create and install Jaeger export pipeline
	flush, err := jaeger.InstallNewPipeline(
		jaeger.WithCollectorEndpoint(fmt.Sprintf("http://%s/api/traces?format=zipkin.thrift", colectorHostname)),
		jaeger.WithProcess(jaeger.Process{
			ServiceName: serviceName,
			Tags: []kv.KeyValue{
				kv.String("commit-hash", os.Getenv("COMMIT_HASH")),
			},
		}),
		jaeger.WithSDK(&sdktrace.Config{DefaultSampler: sdktrace.AlwaysSample()}),
	)
	if err != nil {
		log.Fatal(err)
	}

	return func() {
		flush()
	}
}
