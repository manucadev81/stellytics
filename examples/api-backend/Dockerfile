# syntax=docker/dockerfile:1
# Imagem do backend de exemplo (#35). O contexto do build é a raiz do repositório:
#   docker build -f examples/api-backend/Dockerfile .
# Fica fora de apps/ e services/, então o release.yml não a publica.

FROM node:24.22.1-alpine@sha256:06d7adf3e0ef61a13f1d28682726366f97a15aa1888c934d3ecec86c33918b53
RUN corepack enable
WORKDIR /repo
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY examples/api-backend/package.json examples/api-backend/
RUN --mount=type=cache,id=pnpm,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile --prod --filter @stellytics/example-api-backend
COPY examples/api-backend examples/api-backend
WORKDIR /repo/examples/api-backend
USER node
ENV PORT=4010
EXPOSE 4010
CMD ["node", "server.mjs"]
