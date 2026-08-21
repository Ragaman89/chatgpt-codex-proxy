# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.3

FROM golang:${GO_VERSION}-alpine3.23 AS build
WORKDIR /src

RUN apk add --no-cache ca-certificates git

ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/chatgpt-codex-proxy ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=build /out/chatgpt-codex-proxy /usr/local/bin/chatgpt-codex-proxy

ENV PORT=8080
ENV DATA_DIR=/data
ENV GIN_MODE=release

EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/chatgpt-codex-proxy"]
