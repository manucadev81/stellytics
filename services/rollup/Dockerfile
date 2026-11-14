# syntax=docker/dockerfile:1
# Imagem do job de consolidação e retenção das métricas de API (#34). O
# contexto do build é a raiz do repositório:
#   docker build -f services/rollup/Dockerfile .

FROM golang:1.26.9-alpine@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade AS build
WORKDIR /src/services
COPY services/go.mod services/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY services/rollup rollup
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/rollup ./rollup

# Sem shell: o healthcheck usa o próprio binário (rollup -healthcheck).
FROM scratch
COPY --from=build /out/rollup /rollup
ENV LISTEN_ADDR=:8080
USER 65532:65532
ENTRYPOINT ["/rollup"]
