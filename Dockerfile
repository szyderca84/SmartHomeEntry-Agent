FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Wersja musi byc wstrzyknieta takze tutaj, nie tylko w release.yml i Makefile.
# Bez tego kontener raportuje "dev", a panel nie odrozni go od agenta
# zbudowanego lokalnie. Workflow przekazuje --build-arg VERSION=...
ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
      -ldflags="-s -w -X github.com/smarthomeentry/agent/internal/buildinfo.Version=${VERSION}" \
      -o /smarthomeentry-agent ./cmd/agent

FROM alpine:3.19
RUN mkdir -p /etc/smarthomeentry /var/log
COPY --from=builder /smarthomeentry-agent /usr/local/bin/smarthomeentry-agent
ENTRYPOINT ["/usr/local/bin/smarthomeentry-agent"]
