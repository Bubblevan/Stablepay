FROM golang:1.26.5-alpine AS builder
WORKDIR /src
COPY alipay-adapter /src/alipay-adapter
WORKDIR /src/alipay-adapter
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/alipay-adapter ./cmd/server

FROM alpine:latest
RUN apk add --no-cache ca-certificates && addgroup -g 1000 -S appgroup && adduser -u 1000 -S appuser -G appgroup
WORKDIR /app
COPY --from=builder /out/alipay-adapter /app/alipay-adapter
USER appuser
EXPOSE 8086
ENTRYPOINT ["/app/alipay-adapter"]
