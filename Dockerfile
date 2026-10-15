FROM golang:1.26 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /misskey_exporter .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /misskey_exporter /misskey_exporter

EXPOSE 9100

ENTRYPOINT ["/misskey_exporter"]
