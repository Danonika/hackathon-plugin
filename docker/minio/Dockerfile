# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.24.7-alpine3.22@sha256:fc2cff6625f3c1c92e6c85938ac5bd09034ad0d4bc2dfb08278020b68540dbb5 AS build
WORKDIR /src
ADD --checksum=sha256:8819e3e7817e46b7b3798f8f200ead208562e571563c2e040352378031abe9f2 \
    https://codeload.github.com/minio/minio/tar.gz/07c3a429bfed433e49018cb0f78a52145d4bedeb /tmp/minio.tar.gz
RUN tar -xzf /tmp/minio.tar.gz --strip-components=1 && rm /tmp/minio.tar.gz

# Upstream's release build reads these fields from Git. Supply the tag's
# metadata directly because the verified archive contains no .git directory.
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" GOTOOLCHAIN=local \
    go build -mod=readonly -buildvcs=false -tags kqueue -trimpath -p 2 \
      -ldflags="-s -w \
        -X github.com/minio/minio/cmd.Version=2025-09-07T16:13:09Z \
        -X github.com/minio/minio/cmd.ReleaseTag=RELEASE.2025-09-07T16-13-09Z \
        -X github.com/minio/minio/cmd.CommitID=07c3a429bfed433e49018cb0f78a52145d4bedeb \
        -X github.com/minio/minio/cmd.ShortCommitID=07c3a429bfed \
        -X github.com/minio/minio/cmd.CopyrightYear=2025" \
      -o /out/minio .

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
LABEL org.opencontainers.image.source="https://github.com/minio/minio" \
      org.opencontainers.image.revision="07c3a429bfed433e49018cb0f78a52145d4bedeb" \
      org.opencontainers.image.version="RELEASE.2025-09-07T16-13-09Z" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later"
RUN apk add --no-cache ca-certificates curl
COPY --from=build /out/minio /usr/bin/minio
COPY --from=build /src/LICENSE /src/CREDITS /licenses/
ENTRYPOINT ["/usr/bin/minio"]
CMD ["server", "/data", "--console-address", ":9001"]
