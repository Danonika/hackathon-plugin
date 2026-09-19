FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/hackathonkit-mcp ./cmd/hackathonkit-mcp
RUN CGO_ENABLED=0 go build -trimpath -o /out/hackathonkit-demo-api ./cmd/demo-api

FROM alpine:3.20 AS hackathonkit-mcp
RUN apk add --no-cache ca-certificates
WORKDIR /workspace
COPY --from=build /out/hackathonkit-mcp /usr/local/bin/hackathonkit-mcp
ENV HACKATHONKIT_REPO_ROOT=/workspace
ENTRYPOINT ["hackathonkit-mcp"]

FROM alpine:3.20 AS demo-api
RUN apk add --no-cache ca-certificates
WORKDIR /workspace
COPY --from=build /out/hackathonkit-demo-api /usr/local/bin/hackathonkit-demo-api
ENV HACKATHONKIT_REPO_ROOT=/workspace
EXPOSE 8787
ENTRYPOINT ["hackathonkit-demo-api"]
