FROM golang:1.21 AS builder
WORKDIR /workspace
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/api-gateway ./cmd/api-gateway

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=builder /out/api-gateway /app/api-gateway
COPY configs/config.yaml /app/config.yaml
EXPOSE 8080
ENTRYPOINT ["/app/api-gateway","-config","/app/config.yaml"]
