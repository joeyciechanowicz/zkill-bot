FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /eve-notifier .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /eve-notifier /app/eve-notifier
ENTRYPOINT ["/app/eve-notifier"]
CMD ["--config", "/config/config.yaml"]
