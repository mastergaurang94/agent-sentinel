package loopdetect

import (
	"context"
	"net"
	"time"

	pb "embedding-sidecar/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client wraps the gRPC client for the embedding sidecar.
type Client struct {
	client  pb.EmbeddingServiceClient
	timeout time.Duration
}

// New creates a client dialing over UDS with the given timeout.
func New(udsPath string, timeout time.Duration) (*Client, error) {
	if udsPath == "" {
		return nil, nil
	}
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
	}, nil
}

// Check calls the sidecar for loop detection. Fail-open on error.
func (c *Client) Check(ctx context.Context, tenantID, prompt string) (pb.CheckLoopResponse, error) {
	var empty pb.CheckLoopResponse
	if c == nil || c.client == nil || prompt == "" || tenantID == "" {
		return empty, nil
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
		return empty, err
	}
	return *resp, nil
}
