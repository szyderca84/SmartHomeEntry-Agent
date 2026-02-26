FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /smarthomeentry-agent ./cmd/agent

FROM alpine:3.19
RUN mkdir -p /etc/smarthomeentry /var/log
COPY --from=builder /smarthomeentry-agent /usr/local/bin/smarthomeentry-agent
ENTRYPOINT ["/usr/local/bin/smarthomeentry-agent"]
