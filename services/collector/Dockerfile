# syntax=docker/dockerfile:1
# Imagem do coletor de métricas de API (#30). O contexto do build é a raiz do
# repositório:
#   docker build -f services/collector/Dockerfile .

FROM golang:1.26.11-alpine@sha256:ecb67f987aa1a57794c91d19ade7ae0209f2f0f5496aaef9e6ae85ddf92d02ab AS build
WORKDIR /src/services
COPY services/go.mod services/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY services/collector collector
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/notify-collector ./collector/cmd/notify-collector && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./collector/cmd/healthcheck

# Sem shell nem gerenciador de pacotes: os dois binários e a configuração.
FROM scratch
COPY --from=build /out/notify-collector /notify-collector
COPY --from=build /out/healthcheck /healthcheck
COPY services/collector/config.yaml /etc/notify-collector/config.yaml
USER 65532:65532
EXPOSE 4317 4318
ENTRYPOINT ["/notify-collector"]
CMD ["--config", "/etc/notify-collector/config.yaml"]
