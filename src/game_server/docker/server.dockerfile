# STAGE 1: BUILDER
FROM golang:1.24.5 AS builder
WORKDIR /app
COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY . .
# Compila o binário
RUN CGO_ENABLED=0 GOOS=linux go build -o server_app .

# STAGE 2: FINAL
FROM alpine:latest
# Adiciona certificados para HTTPS e tzdata para timezone
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/server_app .

# (Opcional) Se tiver o healthcheck.sh
COPY docker/healthcheck.sh .
RUN chmod +x healthcheck.sh

# EXPOSE é apenas documentação no modo host, mas é boa prática manter
EXPOSE 8080

CMD ["./server_app"]