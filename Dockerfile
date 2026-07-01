# Multi-stage build producing a small static-binary image for Linux validation.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=docker
ARG COMMIT=none
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w \
      -X github.com/stansat/proby/internal/version.Version=${VERSION} \
      -X github.com/stansat/proby/internal/version.Commit=${COMMIT}" \
    -o /out/proby ./cmd/proby

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=build /out/proby /usr/local/bin/proby
COPY proby.example.yml /etc/proby/proby.yml
EXPOSE 8080
# ICMP: containers run as root with CAP_NET_RAW by default, which satisfies the raw
# socket fallback. If you drop capabilities, add --cap-add=NET_RAW or widen
# net.ipv4.ping_group_range.
ENTRYPOINT ["proby"]
CMD ["-c", "/etc/proby/proby.yml", "run"]
