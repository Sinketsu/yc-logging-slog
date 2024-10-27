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
	t.Parallel()

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

func TestSpecialTypes(t *testing.T) {
	t.Parallel()

	handler, err := New(Options{
		FolderId:     "test-folder",
		ResourceType: "test",
		ResourceId:   "slog-contract",
		Credentials:  ycsdk.OAuthToken("test-token"),
	})
	require.NoError(t, err)

	ms := newMockServer(t)
	handler.log = ms

	logger := slog.New(handler)

	sliceField := []string{"foo", "bar"}
	doubleSliceField := [][]int{{42}, {84}}
	arrayField := [2]float64{0.1, 0.2}
	mapField := map[string]int{"a": 10, "b": 20}

	logger.With(
		"slice", sliceField,
		"doubleSlice", doubleSliceField,
		"array", arrayField,
		"map", mapField,
	).Info("test special cases")
	require.Eventually(t, func() bool {
		return len(ms.getEntries()) > 0
	}, 10*time.Second, 100*time.Millisecond)

	gotFields := ms.getEntries()[0].JsonPayload.AsMap()
	assert.ElementsMatch(t, sliceField, gotFields["slice"])
	assert.ElementsMatch(t, arrayField, gotFields["array"])

	// custom assertion of map, because of testify/assert can't compare map[string]int{} and map[string]any{}
	gotMap, ok := gotFields["map"].(map[string]any)
	assert.True(t, ok, "field `map` is not a map[string]any")
	assert.Equal(t, len(mapField), len(gotMap), "lengths are different")
	for k1, v1 := range mapField {
		if v2, ok := gotMap[k1]; !ok || assert.ObjectsAreEqual(v1, v2) {
			assert.Fail(t, "elements %v and %v are different", v1, v2)
		}
	}

	// custom assertiong for [][]int, because of testify/assert can't compare it properly
	gotDoubleSlice, ok := gotFields["doubleSlice"].([]any)
	assert.True(t, ok, "type of `doubleSlice` field is not []any")
	assert.Equal(t, len(doubleSliceField), len(gotDoubleSlice), "lengths are different")
	for i := range len(doubleSliceField) {
		iSlice, ok := gotDoubleSlice[i].([]any)
		assert.True(t, ok, "type of `doubleSlice[%d]` field is not []any", i)
		assert.Equal(t, len(doubleSliceField[i]), len(iSlice), "lengths are different")
		for j := range len(doubleSliceField[i]) {
			assert.EqualValues(t, doubleSliceField[i][j], iSlice[j])
		}
	}
}

func TestCustomStruct(t *testing.T) {
	t.Parallel()

	handler, err := New(Options{
		FolderId:     "test-folder",
		ResourceType: "test",
		ResourceId:   "slog-contract",
		Credentials:  ycsdk.OAuthToken("test-token"),
	})
	require.NoError(t, err)

	ms := newMockServer(t)
	handler.log = ms

	logger := slog.New(handler)

	type MyStruct struct {
		IntField    int64
		StringField string
	}

	my := MyStruct{IntField: 42, StringField: "test"}
	logger.With("struct", my, slog.Duration("dur", 2*time.Second)).Info("test custom struct")
	require.Eventually(t, func() bool {
		return len(ms.getEntries()) > 0
	}, 10*time.Second, 100*time.Millisecond)

	gotFields := ms.getEntries()[0].JsonPayload.AsMap()
	gotStruct, ok := gotFields["struct"].(map[string]any)
	assert.True(t, ok, "type of `struct` field is not map[string]any")
	assert.EqualValues(t, 42, gotStruct["IntField"])
	assert.EqualValues(t, "test", gotStruct["StringField"])
}

func TestAllSlogTypes(t *testing.T) {
	t.Parallel()

	handler, err := New(Options{
		FolderId:     "test-folder",
		ResourceType: "test",
		ResourceId:   "slog-contract",
		Credentials:  ycsdk.OAuthToken("test-token"),
	})
	require.NoError(t, err)

	ms := newMockServer(t)
	handler.log = ms

	logger := slog.New(handler)
	logger.With(
		slog.Bool("bool", true),
		slog.Duration("duration", 2*time.Second),
		slog.Float64("float", 2.42),
		slog.Int("int", 42),
		slog.Int64("int64", 420),
		slog.String("string", "test string"),
		slog.Time("time", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
		slog.Uint64("uint64", 24),
	).Info("test")

	require.Eventually(t, func() bool {
		return len(ms.getEntries()) > 0
	}, 10*time.Second, 100*time.Millisecond)

	gotFields := ms.getEntries()[0].JsonPayload.AsMap()
	assert.EqualValues(t, true, gotFields["bool"])

	dur, err := time.ParseDuration(gotFields["duration"].(string))
	assert.NoError(t, err)
	assert.EqualValues(t, 2*time.Second, dur)

	assert.EqualValues(t, 2.42, gotFields["float"])
	assert.EqualValues(t, 42, gotFields["int"])
	assert.EqualValues(t, 420, gotFields["int64"])
	assert.EqualValues(t, "test string", gotFields["string"])

	parsedTime, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", gotFields["time"].(string))
	assert.NoError(t, err)
	assert.EqualValues(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), parsedTime)

	assert.EqualValues(t, 24, gotFields["uint64"])
}
