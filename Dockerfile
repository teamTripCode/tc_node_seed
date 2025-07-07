# Build stage
FROM golang:1.24-alpine AS builder

# Instalar dependencias necesarias + herramientas de seguridad
RUN apk add --no-cache git ca-certificates tzdata \
    openssl \
    curl \
    && rm -rf /var/cache/apk/*

# Crear usuario no-root
RUN adduser -D -s /bin/sh -u 1000 appuser

WORKDIR /app

# Copiar archivos de módulos Go primero (para aprovechar cache de Docker)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copiar todo el código fuente
COPY . .

# Build con optimizaciones y flags de seguridad
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -trimpath \
    -o /seed-node ./main.go

# Verificar binario
RUN file /seed-node && ldd /seed-node || true

# Final stage - usando imagen distroless para mayor seguridad
FROM gcr.io/distroless/static-debian12:nonroot

# Copiar certificados y zona horaria
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copiar binario
COPY --from=builder /seed-node /usr/local/bin/seed-node

# Usar usuario no-root de distroless (UID 65532)
USER 65532:65532

# Establecer directorio de trabajo
WORKDIR /app

# Variables de entorno por defecto para nodo semilla
ENV DATA_DIR=/app/data \
    LOG_DIR=/app/logs \
    CONFIG_DIR=/app/config \
    P2P_PORT=3000 \
    API_PORT=3001 \
    METRICS_PORT=9090 \
    LIBP2P_PORT=4000 \
    NODE_TYPE="seed" \
    SEED_MODE="true" \
    # Variables de seguridad
    SECURITY_KEYSTORE_PATH="/app/data/keystore" \
    SECURITY_LOG_LEVEL="info" \
    SECURITY_RATE_LIMIT="200" \
    SECURITY_MAX_CONNECTIONS="100" \
    # Configuración específica para nodo semilla
    BOOTSTRAP_ENABLED="true" \
    PEER_DISCOVERY_ENABLED="true" \
    DHT_ENABLED="true" \
    SEED_ANNOUNCE_INTERVAL="30s"

# Puertos a exponer
EXPOSE ${P2P_PORT} ${API_PORT} ${METRICS_PORT} ${LIBP2P_PORT}

# Volúmenes para datos persistentes
VOLUME ["/app/data", "/app/logs", "/app/config"]

# Health check específico para nodo semilla
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=5 \
    CMD ["/usr/local/bin/seed-node", "--health-check"] || exit 1

# Usar array form para CMD (más seguro)
CMD ["/usr/local/bin/seed-node"]