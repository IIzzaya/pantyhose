FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /pantyhose-server ./cmd/pantyhose-server
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /pantyhose-client ./cmd/pantyhose-client

# --- Server image ---
FROM alpine:3.20 AS server
RUN apk add --no-cache ca-certificates
COPY --from=builder /pantyhose-server /usr/local/bin/pantyhose-server
ENTRYPOINT ["pantyhose-server"]

# --- Client image ---
FROM alpine:3.20 AS client
RUN apk add --no-cache ca-certificates
COPY --from=builder /pantyhose-client /usr/local/bin/pantyhose-client
ENTRYPOINT ["pantyhose-client"]

# --- Test runner ---
FROM golang:1.24-alpine AS test
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
CMD ["go", "test", "-v", "-count=1", "-timeout", "120s", "./..."]
