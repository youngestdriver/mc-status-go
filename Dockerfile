# syntax=docker/dockerfile:1

FROM golang:1.23.4-alpine3.21 AS builder

WORKDIR /src

ARG GOPROXY=https://goproxy.cn|https://goproxy.io|https://proxy.golang.org|direct
ENV GOPROXY=${GOPROXY}

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/mc-status-api ./cmd/api

FROM alpine:3.21

RUN addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=builder /out/mc-status-api /app/mc-status-api

USER app

EXPOSE 8080

ENTRYPOINT ["/app/mc-status-api"]
CMD ["-listen", ":8080", "-timeout", "5s"]
