FROM golang:1.27-alpine3.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/hackathonkit-mcp ./cmd/hackathonkit-mcp
RUN CGO_ENABLED=0 go build -trimpath -o /out/hackathonkit-demo-api ./cmd/demo-api

FROM alpine:3.24.2 AS runtime-base
RUN apk add --no-cache ca-certificates && adduser -D -u 1000 hackathonkit
WORKDIR /workspace
RUN mkdir -p /workspace/configs /workspace/logs /workspace/data/input /workspace/data/artifacts /workspace/data/index && chown -R 1000:1000 /workspace
ENV HACKATHONKIT_REPO_ROOT=/workspace
USER 1000:1000

FROM runtime-base AS hackathonkit-mcp
COPY --from=build /out/hackathonkit-mcp /usr/local/bin/hackathonkit-mcp
ENTRYPOINT ["hackathonkit-mcp"]

FROM runtime-base AS demo-api
COPY --from=build /out/hackathonkit-demo-api /usr/local/bin/hackathonkit-demo-api
EXPOSE 8787
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD wget -qO- "http://127.0.0.1:${HACKATHONKIT_DEMO_PORT:-8787}/healthz" || exit 1
ENTRYPOINT ["hackathonkit-demo-api"]

# Go-enabled gate image: runs scripts/quality-gate.sh from the bind mount and
# stores evidence under evals/quality/. Use with `docker compose run --rm quality`.
FROM golang:1.27-alpine3.24 AS quality
RUN apk add --no-cache git
WORKDIR /workspace
ENTRYPOINT ["sh", "scripts/quality-gate.sh"]
