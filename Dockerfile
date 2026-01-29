FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=0.1.0" -o /bin/docker-release ./cmd/docker-release/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/docker-release /usr/local/bin/docker-release
COPY builds/ /builds/
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
CMD ["watch"]
