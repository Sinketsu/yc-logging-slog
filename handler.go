package ycloggingslog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/yandex-cloud/go-genproto/yandex/cloud/logging/v1"
	ycsdk "github.com/yandex-cloud/go-sdk"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	Stream = "!_ycloggingslog.Stream_"
)

func loggingLevel(level slog.Level) logging.LogLevel_Level {
	switch {
	case level >= slog.LevelError:
		return logging.LogLevel_ERROR
	case level >= slog.LevelWarn:
		return logging.LogLevel_WARN
	case level >= slog.LevelInfo:
		return logging.LogLevel_INFO
	default:
		return logging.LogLevel_DEBUG
	}
}

type Handler struct {
	data        map[string]any
	groups      []string
	level       slog.Level
	ch          chan *logging.IncomingLogEntry
	destination *logging.Destination
	resource    *logging.LogEntryResource
	stream      string
	log         logging.LogIngestionServiceClient
}

func New(options Options) (*Handler, error) {
	var destination *logging.Destination

	switch {
	case options.FolderId == "" && options.LogGroupId == "":
		return nil, fmt.Errorf("you must set one of FolderId or LogGroupId")
	case options.FolderId != "" && options.LogGroupId != "":
		return nil, fmt.Errorf("you must set only one of FolderId or LogGroupId")
	case options.FolderId != "":
		destination = &logging.Destination{
			Destination: &logging.Destination_FolderId{
				FolderId: options.FolderId,
			},
		}
	case options.LogGroupId != "":
		destination = &logging.Destination{
			Destination: &logging.Destination_LogGroupId{
				LogGroupId: options.LogGroupId,
			},
		}
	}

	sdk, err := ycsdk.Build(context.Background(), ycsdk.Config{
		Credentials: options.Credentials,
	})
	if err != nil {
		return nil, fmt.Errorf("fail to build sdk: %w", err)
	}

	if options.BufferSize == 0 {
		options.BufferSize = 100
	}

	handler := &Handler{
		data:        make(map[string]any),
		level:       options.Level,
		ch:          make(chan *logging.IncomingLogEntry, options.BufferSize),
		log:         sdk.LogIngestion().LogIngestion(),
		destination: destination,
		resource: &logging.LogEntryResource{
			Type: options.ResourceType,
			Id:   options.ResourceId,
		},
	}

	go handler.start(options.BufferSize)

	return handler, nil
}

func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}

	clone := h.clone()
	clone.appendLastGroup(attrs...)

	return &clone
}

func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	clone := h.clone()
	clone.groups = append(clone.groups, name)

	return &clone
}

func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	data := h.data

	if r.NumAttrs() > 0 {
		attrs := make([]slog.Attr, 0, r.NumAttrs())
		r.Attrs(func(a slog.Attr) bool {
			attrs = append(attrs, a)
			return true
		})

		tmp := &Handler{data: maps.Clone(h.data), groups: slices.Clone(h.groups)}
		tmp.appendLastGroup(attrs...)
		data = tmp.data
	}

	payload, err := structpb.NewStruct(data)
	if err != nil {
		return err
	}

	h.ch <- &logging.IncomingLogEntry{
		Timestamp:   timestamppb.New(r.Time),
		Level:       loggingLevel(r.Level),
		Message:     r.Message,
		JsonPayload: payload,
		StreamName:  h.stream,
	}

	return nil
}

func (h *Handler) start(size int) {
	buffer := make([]*logging.IncomingLogEntry, 0, size)

	for {
		select {
		case <-time.After(2 * time.Second):
			h.flush(buffer)
			buffer = buffer[:0]
		case entry := <-h.ch:
			buffer = append(buffer, entry)

			if len(buffer) >= size {
				h.flush(buffer)
				buffer = buffer[:0]
			}
		}
	}
}

func (h *Handler) flush(buffer []*logging.IncomingLogEntry) {
	if len(buffer) == 0 {
		return
	}

	entries := slices.Clone(buffer)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := h.log.Write(ctx, &logging.WriteRequest{
		Destination: h.destination,
		Resource:    h.resource,
		Entries:     entries,
	})
	if err != nil {
		fmt.Println("error", err)
	}
}

func (h *Handler) appendLastGroup(attrs ...slog.Attr) {
	current := h.data
	for _, g := range h.groups {
		child, ok := current[g].(map[string]any)
		if ok {
			current = child
		} else {
			child = make(map[string]any)
			current[g] = child
			current = child
		}
	}

	h.appendData(current, attrs...)
}

func (h *Handler) appendData(currentData map[string]any, attrs ...slog.Attr) {
	for _, a := range attrs {
		if !a.Equal(slog.Attr{}) {
			if a.Key == Stream {
				h.stream = a.Value.Resolve().String()
				continue
			}

			switch resolved := a.Value.Resolve(); resolved.Kind() {
			case slog.KindGroup:
				if a.Key == "" {
					h.appendData(currentData, resolved.Group()...)
				} else {
					group := make(map[string]any)
					h.appendData(group, resolved.Group()...)
					currentData[a.Key] = group
				}
			case slog.KindAny:
				value, err := convertStructpb(resolved.Any())
				if err != nil {
					currentData[a.Key] = "<error: " + err.Error() + ">"
					continue
				}

				currentData[a.Key] = value.AsInterface()
			case slog.KindDuration:
				currentData[a.Key] = resolved.Duration().String()
			case slog.KindTime:
				currentData[a.Key] = resolved.Time().String()
			default:
				currentData[a.Key] = resolved.Any()
			}
		}
	}
}

func (h *Handler) clone() Handler {
	return Handler{
		data:        maps.Clone(h.data),
		groups:      slices.Clone(h.groups),
		level:       h.level,
		ch:          h.ch,
		destination: h.destination,
		resource:    h.resource,
		stream:      h.stream,
		log:         h.log,
	}
}

// Hack to get valid structpb from any value
func convertStructpb(v any) (*structpb.Value, error) {
	to := &structpb.Value{}

	bytes, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	err = protojson.Unmarshal(bytes, to)
	if err != nil {
		return nil, err
	}

	return to, nil
}
