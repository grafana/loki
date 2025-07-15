package deletion

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/tenant"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	compactor_client_grpc "github.com/grafana/loki/v3/pkg/compactor/client/grpc"
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
			t.Logf("Failed to serve: %v", err)
		}
	}()

	// nolint:staticcheck // grpc.DialContext() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.DialContext(context.Background(), "",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	closer := func() {
		require.NoError(t, lis.Close())
		baseServer.GracefulStop()
	}

	client := compactor_client_grpc.NewCompactorClient(conn)

	return client, closer
}

func grpcDeleteRequestsToDeleteRequests(requests []*compactor_client_grpc.DeleteRequest) []DeleteRequest {
	resp := make([]DeleteRequest, len(requests))
	for i, grpcReq := range requests {
		resp[i] = DeleteRequest{
			RequestID: grpcReq.RequestID,
			StartTime: model.Time(grpcReq.StartTime),
			EndTime:   model.Time(grpcReq.EndTime),
			Query:     grpcReq.Query,
			Status:    DeleteRequestStatus(grpcReq.Status),
			CreatedAt: model.Time(grpcReq.CreatedAt),
		}
	}

	return resp
}

func TestGRPCGetDeleteRequests(t *testing.T) {
	t.Run("it gets all the delete requests for the user", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllResult = []DeleteRequest{{RequestID: "test-request-1", Status: StatusReceived}, {RequestID: "test-request-2", Status: StatusReceived}}
		h := NewGRPCRequestHandler(store, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}})
		grpcClient, closer := server(t, h)
		t.Cleanup(closer)

		ctx, _ := user.InjectIntoGRPCRequest(user.InjectOrgID(context.Background(), user1))
		orgID, err := tenant.TenantID(ctx)
		require.NoError(t, err)
		require.Equal(t, user1, orgID)

		resp, err := grpcClient.GetDeleteRequests(ctx, &compactor_client_grpc.GetDeleteRequestsRequest{})
		require.NoError(t, err)
		require.ElementsMatch(t, store.getAllResult, grpcDeleteRequestsToDeleteRequests(resp.DeleteRequests))
	})

	t.Run("it merges requests with the same requestID", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllResult = []DeleteRequest{
			{RequestID: "test-request-1", CreatedAt: now, StartTime: now, EndTime: now.Add(time.Hour)},
			{RequestID: "test-request-1", CreatedAt: now, StartTime: now.Add(2 * time.Hour), EndTime: now.Add(3 * time.Hour)},
			{RequestID: "test-request-2", CreatedAt: now.Add(time.Minute), StartTime: now.Add(30 * time.Minute), EndTime: now.Add(90 * time.Minute)},
			{RequestID: "test-request-1", CreatedAt: now, StartTime: now.Add(time.Hour), EndTime: now.Add(2 * time.Hour)},
		}
		h := NewGRPCRequestHandler(store, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}})
		grpcClient, closer := server(t, h)
		t.Cleanup(closer)

		ctx, _ := user.InjectIntoGRPCRequest(user.InjectOrgID(context.Background(), user1))
		orgID, err := tenant.TenantID(ctx)
		require.NoError(t, err)
		require.Equal(t, user1, orgID)

		resp, err := grpcClient.GetDeleteRequests(ctx, &compactor_client_grpc.GetDeleteRequestsRequest{})
		require.NoError(t, err)
		require.ElementsMatch(t, []DeleteRequest{
			{RequestID: "test-request-1", Status: StatusReceived, CreatedAt: now, StartTime: now, EndTime: now.Add(3 * time.Hour)},
			{RequestID: "test-request-2", Status: StatusReceived, CreatedAt: now.Add(time.Minute), StartTime: now.Add(30 * time.Minute), EndTime: now.Add(90 * time.Minute)},
		}, grpcDeleteRequestsToDeleteRequests(resp.DeleteRequests))
	})

	t.Run("it only considers a request processed if all it's subqueries are processed", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllResult = []DeleteRequest{
			{RequestID: "test-request-1", CreatedAt: now, Status: StatusProcessed},
			{RequestID: "test-request-1", CreatedAt: now, Status: StatusReceived},
			{RequestID: "test-request-1", CreatedAt: now, Status: StatusProcessed},
			{RequestID: "test-request-2", CreatedAt: now.Add(time.Minute), Status: StatusProcessed},
			{RequestID: "test-request-2", CreatedAt: now.Add(time.Minute), Status: StatusProcessed},
			{RequestID: "test-request-2", CreatedAt: now.Add(time.Minute), Status: StatusProcessed},
			{RequestID: "test-request-3", CreatedAt: now.Add(2 * time.Minute), Status: StatusReceived},
		}
		h := NewGRPCRequestHandler(store, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}})
		grpcClient, closer := server(t, h)
		t.Cleanup(closer)

		ctx, _ := user.InjectIntoGRPCRequest(user.InjectOrgID(context.Background(), user1))
		orgID, err := tenant.TenantID(ctx)
		require.NoError(t, err)
		require.Equal(t, user1, orgID)

		resp, err := grpcClient.GetDeleteRequests(ctx, &compactor_client_grpc.GetDeleteRequestsRequest{})
		require.NoError(t, err)
		require.ElementsMatch(t, []DeleteRequest{
			{RequestID: "test-request-1", CreatedAt: now, Status: "66% Complete"},
			{RequestID: "test-request-2", CreatedAt: now.Add(time.Minute), Status: StatusProcessed},
			{RequestID: "test-request-3", CreatedAt: now.Add(2 * time.Minute), Status: StatusReceived},
		}, grpcDeleteRequestsToDeleteRequests(resp.DeleteRequests))
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
