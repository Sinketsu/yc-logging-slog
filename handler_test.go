package ycloggingslog

import (
	"context"
	"log/slog"
	"maps"
	"sync"
	"testing"
	"testing/slogtest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yandex-cloud/go-genproto/yandex/cloud/logging/v1"
	ycsdk "github.com/yandex-cloud/go-sdk"
	"google.golang.org/grpc"
)

type mockServer struct {
	logging.UnimplementedLogIngestionServiceServer

	t       *testing.T
	entries []*logging.IncomingLogEntry
	mu      sync.Mutex
}

func newMockServer(t *testing.T) *mockServer {
	return &mockServer{
		t: t,
	}
}

func (s *mockServer) Write(ctx context.Context, r *logging.WriteRequest, _ ...grpc.CallOption) (*logging.WriteResponse, error) {
	assert.Equal(s.t, "test-folder", r.Destination.GetFolderId())
	assert.Equal(s.t, "test", r.Resource.GetType())
	assert.Equal(s.t, "slog-contract", r.Resource.GetId())

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, r.GetEntries()...)

	return &logging.WriteResponse{}, nil
}

func (s *mockServer) getEntries() []*logging.IncomingLogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.entries
}

func TestSlogContract(t *testing.T) {
	handler, err := New(Options{
		FolderId:     "test-folder",
		ResourceType: "test",
		ResourceId:   "slog-contract",
		Credentials:  ycsdk.OAuthToken("test-token"),
	})
	require.NoError(t, err)

	ms := newMockServer(t)
	handler.log = ms

	err = slogtest.TestHandler(handler, func() []map[string]any {
		require.Eventually(t, func() bool {
			return len(ms.getEntries()) > 0
		}, 10*time.Second, 100*time.Millisecond)

		entries := ms.getEntries()
		results := make([]map[string]any, 0, len(entries))

		for _, entry := range entries {
			result := maps.Clone(entry.JsonPayload.AsMap())
			result[slog.MessageKey] = entry.Message
			result[slog.LevelKey] = entry.Level

			if ts := entry.Timestamp.AsTime(); !ts.IsZero() {
				result[slog.TimeKey] = ts
			}

			results = append(results, result)
		}

		return results
	})
	if err != nil {
		t.Error(err)
	}
}
