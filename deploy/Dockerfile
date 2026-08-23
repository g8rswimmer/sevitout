FROM golang:1.25 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/server ./cmd/server
RUN CGO_ENABLED=0 go build -o /app/slackbot ./cmd/slackbot

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app/server /app/server
COPY --from=builder /app/slackbot /app/slackbot
EXPOSE 8080
# Default to the API server so `docker run <image>` with no explicit command
# still works, matching this image's pre-M11 behavior. This is a CMD, not an
# ENTRYPOINT: deploy/docker-compose.yml's `command:` for each service (e.g.
# the slackbot service's `command: ["/app/slackbot"]`) fully replaces it —
# an ENTRYPOINT would instead have those commands appended as arguments to
# it, running the wrong binary.
CMD ["/app/server"]
