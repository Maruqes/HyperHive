FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /hh-support ./cmd/hh-support

FROM debian:bookworm-slim
RUN mkdir /data && chown 10001:10001 /data
COPY --from=build /hh-support /usr/local/bin/hh-support
USER 10001:10001
ENV SUPPORT_LISTEN=127.0.0.1:8091 SUPPORT_DB_PATH=/data/support.db SQLITE_TMPDIR=/data
VOLUME ["/data"]
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=6 CMD ["/usr/local/bin/hh-support", "--healthcheck"]
CMD ["/usr/local/bin/hh-support"]
