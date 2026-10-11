# syntax=docker/dockerfile:1
FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
ARG VERSION=dev
RUN CGO_ENABLED=1 go build -buildvcs=false -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/altasci-server ./cmd/altasci-server

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 altasci \
    && useradd --uid 10001 --gid altasci --no-create-home --shell /usr/sbin/nologin altasci \
    && install -d -o altasci -g altasci -m 0750 /var/lib/altasci /var/lib/altasci/objects
WORKDIR /app
COPY --from=build /out/altasci-server /usr/local/bin/altasci-server
COPY docker/config.toml /app/config.toml
USER 10001:10001
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl --fail --silent --show-error --max-time 4 http://127.0.0.1:8080/health/ready || exit 1
ENTRYPOINT ["altasci-server"]
CMD ["serve", "--config", "/app/config.toml"]
