FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/substore .

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=build /out/substore /app/substore
RUN mkdir -p /app/data && useradd --system --uid 10001 substore && chown -R substore:substore /app
USER substore
VOLUME ["/app/data"]
EXPOSE 8080
ENTRYPOINT ["/app/substore"]
