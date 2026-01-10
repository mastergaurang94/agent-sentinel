package loopdetect

import (
	"context"
	"net"
	"time"

	pb "embedding-sidecar/proto"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"agent-sentinel/internal/telemetry"
)

// Client wraps the gRPC client for the embedding sidecar.
type Client struct {
	client  pb.EmbeddingServiceClient
	timeout time.Duration
	tracer  trace.Tracer
}

// New creates a client dialing over UDS with the given timeout.
func New(udsPath string, timeout time.Duration) (*Client, error) {
	if udsPath == "" {
		return nil, nil
	}
	tr := telemetry.Tracer()
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (conn net.Conn, err error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", udsPath)
		}),
	}
	conn, err := grpc.Dial("unix://"+udsPath, dialOpts...)
	if err != nil {
		return nil, err
	}
	return &Client{
		client:  pb.NewEmbeddingServiceClient(conn),
		timeout: timeout,
		tracer:  tr,
	}, nil
}

// Check calls the sidecar for loop detection. Fail-open on error.
func (c *Client) Check(ctx context.Context, tenantID, prompt string) (pb.CheckLoopResponse, error) {
	var empty pb.CheckLoopResponse
	if c == nil || c.client == nil || prompt == "" || tenantID == "" {
		return empty, nil
	}
	var span trace.Span
	if c.tracer != nil {
		ctx, span = c.tracer.Start(ctx, "loop_detection.call",
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("loop.tenant_id", tenantID),
				attribute.String("loop.transport", "uds"),
				attribute.Int64("loop.timeout_ms", c.timeout.Milliseconds()),
			),
		)
		defer span.End()
	}
	callCtx := ctx
	if c.timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	resp, err := c.client.CheckLoop(callCtx, &pb.CheckLoopRequest{
		TenantId: tenantID,
		Prompt:   prompt,
	})
	if err != nil {
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return empty, err
	}
	if span != nil && resp != nil {
		span.SetAttributes(
			attribute.Bool("loop.detected", resp.GetLoopDetected()),
			attribute.Float64("loop.max_similarity", resp.GetMaxSimilarity()),
		)
	}
	return *resp, nil
}
