FROM golang:1.24-alpine AS builder

ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o /bin/dr ./cmd/docker-release/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/dr /usr/local/bin/dr
LABEL org.opencontainers.image.title="docker-release"
ENTRYPOINT ["dr"]
CMD ["watch"]
