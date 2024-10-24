# yc-logging-slog

Go slog handler for [Yandex Cloud Logging](https://yandex.cloud/ru/docs/logging/).

> [!CAUTION]
> Not ready for production

But may be useful for your pet projects)

## Usage

1. Get lib:
   `go get github.com/Sinketsu/yc-logging-slog`

2. Use in your code:
    ```go
    package main

    import (
        "log/slog"

        "github.com/Sinketsu/yc-logging-slog"
    )

    func main() {
        opts := ycloggingslog.Options{
            LogGroupId: "<log-group-id>",
            // or
            FolderId: "<folder-id>",

            Credentials: ycsdk.OAuthToken("<oauth-token>"),

            // https://yandex.cloud/ru/docs/logging/api-ref/grpc/LogIngestion/write#yandex.cloud.logging.v1.WriteRequest
            // default - empty
            ResourceType: "<resource-type>",
            // https://yandex.cloud/ru/docs/logging/api-ref/grpc/LogIngestion/write#yandex.cloud.logging.v1.WriteRequest
            // default - empty
            ResourceId: "<resource-id>",

            // default - slog.LevelInfo
            Level: slog.LevelDebug,
            // default - 100
            BufferSize: 200,
        }
        handler, err := ycloggingslog.New(opts)
        // check err
        logger := slog.New(handler)

        logger.With(slog.String("k1", "v1")).Info("Hello, World!", "answer", 42)
    }
    ```
