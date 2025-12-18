# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS builder
WORKDIR /src
# Copy shared auth-client first (needed for replace directive)
# Build context should be from workspace root: docker build -f projects-service/Dockerfile -t projects-service:local .
COPY shared/auth-client /shared/auth-client
COPY projects-service/go.mod projects-service/go.sum ./
RUN go mod download
COPY projects-service .

RUN CGO_ENABLED=0 go build -o /out/projects ./cmd/api

FROM alpine:3.20
RUN addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=builder /out/projects /app/service
# TLS certificates directory (optional, can be mounted as volume)
RUN mkdir -p ./config/certs
USER app
EXPOSE 4005
ENV PORT=4005
ENTRYPOINT ["/app/service"]

