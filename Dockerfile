# syntax=docker/dockerfile:1

FROM golang:1.27-3-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server .


FROM alpine:latest

RUN addgroup -S -g 10001 app \
    && adduser -S -u 10001 -G app app \
    && mkdir -p /data \
    && chown app:app /data

COPY --from=build /out/server /usr/local/bin/server

USER app

ENV PORT=8080 \
    GLOBAL_PREFIX=/api/v2 \
    SQLITE_PATH=/data/excalidraw.db \
    BODY_LIMIT=50mb \
    LOG_LEVEL=info

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/server"]
