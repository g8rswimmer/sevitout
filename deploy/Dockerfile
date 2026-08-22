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
