# One Dockerfile builds any service: the repo is a single module, so only the
# main package differs. SERVICE names the directory under cmd/.
#
# GO_VERSION must satisfy the go directive in go.mod. The official images pin
# GOTOOLCHAIN=local, so unlike a local build they will not fetch a newer
# toolchain on their own - a stale value here fails the build outright rather
# than silently building with the wrong compiler.
ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

# Dependencies are copied first so a code change does not invalidate the
# module download layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG SERVICE
# CGO is off so the binary runs on a scratch-like base with no libc.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/service ./cmd/${SERVICE}

FROM alpine:3.20 AS final

# Certificates are needed for any outbound TLS; adduser avoids running as root.
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 service

COPY --from=build /out/service /usr/local/bin/service

USER service
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/service"]
