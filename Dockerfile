FROM golang:1.26-bookworm AS build

ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY *.go ./
COPY web ./web

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/substore .


FROM debian:bookworm-slim

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/substore /app/substore

RUN mkdir -p /app/data \
    && useradd --system --uid 10001 substore \
    && chown -R substore:substore /app

USER substore

VOLUME ["/app/data"]

EXPOSE 8080

ENTRYPOINT ["/app/substore"]
