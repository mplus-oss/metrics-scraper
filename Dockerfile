FROM golang:1.21.1-alpine AS builder
WORKDIR /app
RUN apk add --no-cache gcc musl-dev
COPY . .
RUN set -ex; \
    go get; \
    go build -ldflags="-linkmode external -extldflags -static -w -s"

FROM busybox:latest
COPY --from=builder /app/metrics-scraper /
USER nobody
ENTRYPOINT ["/metrics-scraper"]
EXPOSE 7878