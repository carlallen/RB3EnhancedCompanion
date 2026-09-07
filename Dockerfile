# syntax=docker/dockerfile:1

FROM golang:1.18-alpine AS build

RUN apk add --no-cache gcc musl-dev

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# go-sqlite3 uses cgo, so CGO must stay enabled.
RUN CGO_ENABLED=1 GOOS=linux go build -o /out/rb3ecompanion ./cmd/server

FROM alpine:3.20

WORKDIR /app

COPY --from=build /out/rb3ecompanion ./rb3ecompanion
COPY web ./web

RUN mkdir -p /store/art

ENV ADDR=:8080 \
    UDP_ADDR=:21070 \
    STORE_DIR=/store

VOLUME ["/store"]

EXPOSE 8080/tcp
EXPOSE 21070/udp

ENTRYPOINT ["/app/rb3ecompanion"]
