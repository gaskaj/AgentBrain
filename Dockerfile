FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /agentbrain ./cmd/agentbrain

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -u 1000 agentbrain

COPY --from=builder /agentbrain /usr/local/bin/agentbrain

USER agentbrain

EXPOSE 8080

ENTRYPOINT ["agentbrain"]
CMD ["--config", "/etc/agentbrain/config.yaml"]
