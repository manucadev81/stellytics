# syntax=docker/dockerfile:1
# Imagem do gateway de ingestão web. O contexto do build é a raiz do repositório:
#   docker build -f services/ingest-web/Dockerfile .

FROM golang:1.26.11-alpine@sha256:ecb67f987aa1a57794c91d19ade7ae0209f2f0f5496aaef9e6ae85ddf92d02ab AS build
WORKDIR /src/services
COPY services/go.mod services/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY services/ingest-web ingest-web
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ingest-web ./ingest-web

# Versões publicadas do script (/s/vX.Y.Z/n.js). A pasta published só existe
# depois da #22; sem ela, a imagem sobe com /s/ vazio.
COPY packages/tracker /tracker
RUN mkdir -p /out/scripts && \
    if [ -d /tracker/published ]; then cp -R /tracker/published/. /out/scripts/; fi

# Sem shell nem gerenciador de pacotes: só o binário e os scripts.
# O healthcheck usa o próprio binário (ingest-web -healthcheck).
FROM scratch
COPY --from=build /out/ingest-web /ingest-web
COPY --from=build /out/scripts /scripts
ENV LISTEN_ADDR=:8080 \
    SCRIPT_DIR=/scripts
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/ingest-web"]
