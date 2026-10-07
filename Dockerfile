FROM golang:1.23-alpine AS build
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/servidor   ./aplicacao/servidor \
 && go build -trimpath -ldflags="-s -w" -o /out/motorista  ./aplicacao/motorista \
 && go build -trimpath -ldflags="-s -w" -o /out/passageiro ./aplicacao/passageiro

FROM alpine:3.20 AS servidor
RUN apk add --no-cache tzdata && adduser -D -u 10001 vaijunto
WORKDIR /app
COPY --from=build /out/servidor /app/servidor

RUN mkdir -p /app/dados && chown -R vaijunto /app
USER vaijunto
VOLUME ["/app/dados"]

ENV TCP_ADDR=:8081 \
    HTTP_ADDR=:8080 \
    DADOS_DIR=/app/dados \
    EMPRESA=VAIJUNTO \
    PARES="" \
    TTL_BLOQUEIO=30s \
    TIMEOUT_PAR=3s
EXPOSE 8080 8081
ENTRYPOINT ["/app/servidor"]

FROM alpine:3.20 AS motorista
RUN adduser -D -u 10001 vaijunto
COPY --from=build /out/motorista /app/motorista
USER vaijunto
ENV SERVIDOR=empresa-a:8081
ENTRYPOINT ["/app/motorista"]

FROM alpine:3.20 AS passageiro
RUN apk add --no-cache ncurses && adduser -D -u 10001 vaijunto
COPY --from=build /out/passageiro /app/passageiro
USER vaijunto
ENV SERVIDOR=empresa-a:8081 TERM=xterm
ENTRYPOINT ["/app/passageiro"]