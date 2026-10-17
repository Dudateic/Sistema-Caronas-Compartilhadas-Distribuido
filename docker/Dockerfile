# Estágio de compilação
FROM golang:1.22-alpine AS builder

WORKDIR /build

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /build/bin/servidor ./aplicacao/servidor
RUN CGO_ENABLED=0 GOOS=linux go build -o /build/bin/passageiro ./aplicacao/passageiro
RUN CGO_ENABLED=0 GOOS=linux go build -o /build/bin/motorista ./aplicacao/motorista
RUN CGO_ENABLED=0 GOOS=linux go build -o /build/bin/simulador ./aplicacao/simulador

# Target: Servidor
FROM alpine:3.19 AS servidor
WORKDIR /app
COPY --from=builder /build/bin/servidor /app/servidor
EXPOSE 6060 7070 8080 9090
ENTRYPOINT ["/app/servidor"]

# Target: Passageiro
FROM alpine:3.19 AS passageiro
WORKDIR /app
COPY --from=builder /build/bin/passageiro /app/passageiro
ENTRYPOINT ["/app/passageiro"]

# Target: Motorista
FROM alpine:3.19 AS motorista
WORKDIR /app
COPY --from=builder /build/bin/motorista /app/motorista
ENTRYPOINT ["/app/motorista"]

# Target: Simulador
FROM alpine:3.19 AS simulador
WORKDIR /app
COPY --from=builder /build/bin/simulador /app/simulador
ENTRYPOINT ["/app/simulador"]