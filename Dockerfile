FROM golang:1.27.0-alpine3.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /out/hackathonkit-mcp ./cmd/hackathonkit-mcp
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /out/hackathonkit-demo-api ./cmd/demo-api

FROM alpine:3.24.2 AS runtime-base
# 1000:1000 matches the first regular host user on most Linux installs. Override
# with --build-arg HACKATHONKIT_UID=$(id -u) HACKATHONKIT_GID=$(id -g) (compose does this
# via scripts/docker-check.sh) when your host user differs.
ARG HACKATHONKIT_UID=1000
ARG HACKATHONKIT_GID=1000
RUN apk add --no-cache ca-certificates && addgroup -g "${HACKATHONKIT_GID}" hackathonkit && adduser -D -u "${HACKATHONKIT_UID}" -G hackathonkit hackathonkit
WORKDIR /workspace
RUN mkdir -p /workspace/configs /workspace/logs /workspace/evals /workspace/data/input /workspace/data/artifacts /workspace/data/index /workspace/data/judge-bundles && chown -R "${HACKATHONKIT_UID}:${HACKATHONKIT_GID}" /workspace
ENV HACKATHONKIT_REPO_ROOT=/workspace
USER ${HACKATHONKIT_UID}:${HACKATHONKIT_GID}

FROM runtime-base AS hackathonkit-mcp
COPY --from=build /out/hackathonkit-mcp /usr/local/bin/hackathonkit-mcp
ENTRYPOINT ["hackathonkit-mcp"]

FROM runtime-base AS demo-api
COPY --from=build /out/hackathonkit-demo-api /usr/local/bin/hackathonkit-demo-api
# Bind on all interfaces so `docker run -p 8787:8787` works without extra env.
ENV HACKATHONKIT_DEMO_HOST=0.0.0.0
EXPOSE 8787
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD wget -qO- "http://127.0.0.1:${HACKATHONKIT_DEMO_PORT:-8787}/healthz" || exit 1
ENTRYPOINT ["hackathonkit-demo-api"]

# Go-enabled gate image: runs scripts/quality-gate.sh from the bind mount and
# stores evidence under evals/quality/. Use with `docker compose run --rm quality`.
FROM golang:1.27.0-alpine3.24 AS quality
RUN apk add --no-cache git
WORKDIR /workspace
ENV HACKATHONKIT_REPO_ROOT=/workspace
# Runs as root on purpose: the gate writes evals/quality/ evidence through a
# host bind mount whose ownership is unknown, and it is CI-only tooling.
ENTRYPOINT ["sh", "scripts/quality-gate.sh"]
