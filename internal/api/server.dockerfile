# Dockerfile
FROM golang:1.21.7-alpine3.19-slim

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /server
RUN apk add --no-cache gcc musl-dev

FROM alpine:3.19.1-slim
WORKDIR /app
COPY --from=builder /server /app/server
COPY certs/ /app/certs/

EXPOSE 8080
CMD ["/app/server"]
