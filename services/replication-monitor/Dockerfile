# syntax=docker/dockerfile:1
FROM golang:1.26.11-alpine@sha256:ecb67f987aa1a57794c91d19ade7ae0209f2f0f5496aaef9e6ae85ddf92d02ab AS build
WORKDIR /src/services
COPY services/go.mod services/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY services/replication-monitor replication-monitor
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/replication-monitor ./replication-monitor
FROM scratch
COPY --from=build /out/replication-monitor /replication-monitor
USER 65532:65532
EXPOSE 9187
ENTRYPOINT ["/replication-monitor"]
