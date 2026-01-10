package telemetry

import (
	"net/http"
	"time"

	"agent-sentinel/internal/providers"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type instrumentedTransport struct {
	base     http.RoundTripper
	provider providers.Provider
}

// NewInstrumentedTransport wraps the provided RoundTripper with tracing and metrics.
func NewInstrumentedTransport(provider providers.Provider, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &instrumentedTransport{base: base, provider: provider}
}

func (t *instrumentedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	model := ""
	if t.provider != nil {
		model = t.provider.ExtractModelFromPath(req.URL.Path)
	}

	providerName := ""
	if t.provider != nil {
		providerName = t.provider.Name()
	}

	ctx, span := StartSpan(ctx, "provider.http",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.url", req.URL.String()),
			attribute.String("provider", providerName),
			attribute.String("llm.model", model),
		),
	)
	start := time.Now()
	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	latency := time.Since(start)

	status := 0
	result := "ok"
	if resp != nil {
		status = resp.StatusCode
	}
	if err != nil {
		result = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else if status >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, http.StatusText(status))
	}

	ObserveProviderHTTP(ctx, providerName, model, status, result, latency)
	span.End()
	return resp, err
}
