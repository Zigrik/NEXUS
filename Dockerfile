FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o nexusd cmd/nexusd/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/nexusd .
COPY --from=builder /app/config.yaml .
COPY web ./web

EXPOSE 8443 9000

CMD ["./nexusd", "config.yaml"]