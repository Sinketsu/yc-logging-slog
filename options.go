package ycloggingslog

import (
	"log/slog"

	ycsdk "github.com/yandex-cloud/go-sdk"
)

type Options struct {
	LogGroupId   string
	FolderId     string
	ResourceType string
	ResourceId   string
	Level        slog.Level
	Credentials  ycsdk.Credentials
	BufferSize   int
}
