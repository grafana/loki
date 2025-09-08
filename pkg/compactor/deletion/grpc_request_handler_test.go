package deletion

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/tenant"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	compactor_client_grpc "github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
	"github.com/grafana/loki/v3/pkg/compactor/deletionmode"
)

func server(t *testing.T, h *GRPCRequestHandler) (compactor_client_grpc.CompactorClient, func()) {
	buffer := 101024 * 1024
	lis := bufconn.Listen(buffer)

	baseServer := grpc.NewServer(grpc.ChainStreamInterceptor(func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return middleware.StreamServerUserHeaderInterceptor(srv, ss, info, handler)
	}), grpc.ChainUnaryInterceptor(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		return middleware.ServerUserHeaderInterceptor(ctx, req, info, handler)
	}))

	compactor_client_grpc.RegisterCompactorServer(baseServer, h)
	go func() {
		if err := baseServer.Serve(lis); err != nil {
			require.NoError(t, err)
		}
	}()

	// nolint:staticcheck // grpc.DialContext() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.DialContext(context.Background(), "",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	closer := func() {
		baseServer.GracefulStop()
		require.NoError(t, lis.Close())
	}

	client := compactor_client_grpc.NewCompactorClient(conn)

	return client, closer
}

func TestGRPCGetDeleteRequests(t *testing.T) {
	t.Run("it gets all the delete requests for the user", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllResult = []deletionproto.DeleteRequest{{RequestID: "test-request-1", Status: deletionproto.StatusReceived}, {RequestID: "test-request-2", Status: deletionproto.StatusReceived}}
		h := NewGRPCRequestHandler(store, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}})
		grpcClient, closer := server(t, h)
		t.Cleanup(closer)

		ctx, _ := user.InjectIntoGRPCRequest(user.InjectOrgID(context.Background(), user1))
		orgID, err := tenant.TenantID(ctx)
		require.NoError(t, err)
		require.Equal(t, user1, orgID)

		resp, err := grpcClient.GetDeleteRequests(ctx, &compactor_client_grpc.GetDeleteRequestsRequest{})
		require.NoError(t, err)
		require.Len(t, resp.DeleteRequests, len(store.getAllResult))
		for i := range store.getAllResult {
			require.True(t, requestsAreEqual(store.getAllResult[i], *resp.DeleteRequests[i]))
		}
	})

	t.Run("error getting from store", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllErr = errors.New("something bad")
		h := NewGRPCRequestHandler(store, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}})
		grpcClient, closer := server(t, h)
		t.Cleanup(closer)

		ctx, _ := user.InjectIntoGRPCRequest(user.InjectOrgID(context.Background(), user1))
		orgID, err := tenant.TenantID(ctx)
		require.NoError(t, err)
		require.Equal(t, user1, orgID)

		_, err = grpcClient.GetDeleteRequests(ctx, &compactor_client_grpc.GetDeleteRequestsRequest{})
		require.Error(t, err)
		sts, _ := status.FromError(err)
		require.Equal(t, "something bad", sts.Message())
	})

	t.Run("validation", func(t *testing.T) {
		t.Run("no org id", func(t *testing.T) {
			h := NewGRPCRequestHandler(&mockDeleteRequestsStore{}, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}})
			grpcClient, closer := server(t, h)
			t.Cleanup(closer)

			_, err := grpcClient.GetDeleteRequests(context.Background(), &compactor_client_grpc.GetDeleteRequestsRequest{})
			require.Error(t, err)
			sts, _ := status.FromError(err)
			require.Equal(t, "no org id", sts.Message())
		})
	})
}

func TestGRPCGetCacheGenNumbers(t *testing.T) {
	t.Run("get gen number", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.genNumber = "123"
		h := NewGRPCRequestHandler(store, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}})
		grpcClient, closer := server(t, h)
		t.Cleanup(closer)

		ctx, _ := user.InjectIntoGRPCRequest(user.InjectOrgID(context.Background(), user1))
		orgID, err := tenant.TenantID(ctx)
		require.NoError(t, err)
		require.Equal(t, user1, orgID)

		resp, err := grpcClient.GetCacheGenNumbers(ctx, &compactor_client_grpc.GetCacheGenNumbersRequest{})
		require.NoError(t, err)
		require.Equal(t, store.genNumber, resp.ResultsCacheGen)
	})

	t.Run("error getting from store", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getErr = errors.New("something bad")
		h := NewGRPCRequestHandler(store, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}})
		grpcClient, closer := server(t, h)
		t.Cleanup(closer)

		ctx, _ := user.InjectIntoGRPCRequest(user.InjectOrgID(context.Background(), user1))
		orgID, err := tenant.TenantID(ctx)
		require.NoError(t, err)
		require.Equal(t, user1, orgID)

		_, err = grpcClient.GetCacheGenNumbers(ctx, &compactor_client_grpc.GetCacheGenNumbersRequest{})
		require.Error(t, err)
		sts, _ := status.FromError(err)
		require.Equal(t, "something bad", sts.Message())
	})

	t.Run("validation", func(t *testing.T) {
		t.Run("no org id", func(t *testing.T) {
			h := NewGRPCRequestHandler(&mockDeleteRequestsStore{}, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}})
			grpcClient, closer := server(t, h)
			t.Cleanup(closer)

			_, err := grpcClient.GetCacheGenNumbers(context.Background(), &compactor_client_grpc.GetCacheGenNumbersRequest{})
			require.Error(t, err)
			sts, _ := status.FromError(err)
			require.Equal(t, "no org id", sts.Message())
		})
	})
}
