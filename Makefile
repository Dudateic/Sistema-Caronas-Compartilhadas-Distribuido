COMPOSE       ?= docker compose -f docker/docker-compose.yaml
COMPOSE_LOCAL ?= docker compose -f docker/docker-compose.yaml -f docker/docker-compose.local-ips.yaml
BIN           ?= bin
IP_A          ?= 172.16.201.9
IP_B          ?= 172.16.201.11
IP_C          ?= 172.16.201.12
ALVOS         ?= empresa-a:6060,empresa-b:7070,empresa-c:8080
SIM_IMG       ?= vaijunto-simulador
REDE          ?= docker_consorcio

.DEFAULT_GOAL := help

.PHONY: help build test up-local down-local up down restart logs clean clean-data \
        servidor-a servidor-b servidor-c \
        run-servidor-a run-servidor-b run-servidor-c \
        cliente-passageiro-a cliente-passageiro-b cliente-passageiro-c \
        cliente-motorista-a cliente-motorista-b cliente-motorista-c \
        build-simulador simulador simulador-todos simulador-contencao simulador-cancelamento \
        simulador-motorista simulador-busca \
        down-a down-b down-c up-a up-b up-c

help:
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}'

build: ## compila os binarios locais em ./bin
	@mkdir -p $(BIN)
	go build -o $(BIN)/servidor   ./aplicacao/servidor
	go build -o $(BIN)/motorista  ./aplicacao/motorista
	go build -o $(BIN)/passageiro ./aplicacao/passageiro
	go build -o $(BIN)/simulador  ./aplicacao/simulador-cli

test: ## roda a suite de testes automatizados (go test)
	go test ./tests/... -v -count=1

up-local: ## sobe as 3 empresas em contêineres, com IPs fixos (rede do consórcio)
	COMPOSE_PARALLEL_LIMIT=1 $(COMPOSE_LOCAL) up -d --build empresa-a empresa-b empresa-c

down-local: ## derruba as 3 empresas e a rede
	$(COMPOSE_LOCAL) down

up: ## sobe as 3 empresas com a configuração padrão
	$(COMPOSE) up -d --build empresa-a empresa-b empresa-c

down: ## derruba as 3 empresas com a configuração padrão
	$(COMPOSE) down

restart: down up

logs: ## acompanha os logs das empresas
	$(COMPOSE_LOCAL) logs -f --tail=100 empresa-a empresa-b empresa-c

servidor-a: ## TCP 6060, HTTP 9091 (uso individual, sem o consórcio completo)
	$(COMPOSE) up --build empresa-a

servidor-b: ## TCP 7070, HTTP 9092
	$(COMPOSE) up --build empresa-b

servidor-c: ## TCP 8080, HTTP 9093
	$(COMPOSE) up --build empresa-c

run-servidor-a: build
	@mkdir -p ./dados/empresa_A
	./$(BIN)/servidor -empresa=A -tcp=:6060 -http=:9091 -dados=./dados/empresa_A -pares="B=http://$(IP_B):9092,C=http://$(IP_C):9093"

run-servidor-b: build
	@mkdir -p ./dados/empresa_B
	./$(BIN)/servidor -empresa=B -tcp=:7070 -http=:9092 -dados=./dados/empresa_B -pares="A=http://$(IP_A):9091,C=http://$(IP_C):9093"

run-servidor-c: build
	@mkdir -p ./dados/empresa_C
	./$(BIN)/servidor -empresa=C -tcp=:8080 -http=:9093 -dados=./dados/empresa_C -pares="A=http://$(IP_A):9091,B=http://$(IP_B):9092"

cliente-passageiro-a: ## passageiro conectado a empresa A
	$(COMPOSE) run --rm passageiro-a

cliente-passageiro-b: ## passageiro conectado a empresa B
	$(COMPOSE) run --rm passageiro-b

cliente-passageiro-c: ## passageiro conectado a empresa C
	$(COMPOSE) run --rm passageiro-c

cliente-motorista-a: ## motorista conectado a empresa A
	$(COMPOSE) run --rm motorista-a

cliente-motorista-b: ## motorista conectado a empresa B
	$(COMPOSE) run --rm motorista-b

cliente-motorista-c: ## motorista conectado a empresa C
	$(COMPOSE) run --rm motorista-c

define DOCKERFILE_SIMULADOR
FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod go.sum* ./
RUN go mod download
COPY aplicacao ./aplicacao
COPY componentes ./componentes
RUN CGO_ENABLED=0 GOOS=linux go build -o /build/bin/simulador ./aplicacao/simulador-cli

FROM alpine:3.19 AS simulador
WORKDIR /app
COPY --from=builder /build/bin/simulador /app/simulador
RUN chmod 0755 /app/simulador && ls -l /app/simulador
ENTRYPOINT ["/app/simulador"]
endef
export DOCKERFILE_SIMULADOR

build-simulador: ## constroi a imagem do simulador (so codigo-fonte, sem bin/ do host)
	@printf '%s\n' "$$DOCKERFILE_SIMULADOR" | docker build --no-cache -f - --target simulador -t $(SIM_IMG) .

# Os alvos do simulador sobem as empresas (se nao estiverem no ar), constroem a
# imagem do simulador e a executam na mesma rede dos servidores (REDE).
simulador-contencao: up-local build-simulador ## 200 reservas disputando 5 assentos (leve)
	docker run --rm --cpus=1 --memory=512m --network $(REDE) $(SIM_IMG) -total=200 -concorrencia=5 -alvos="$(ALVOS)" -cenario=contencao -assentos=5

simulador-cancelamento: up-local build-simulador ## 200 operacoes com cancelamento (leve)
	docker run --rm --cpus=1 --memory=512m --network $(REDE) $(SIM_IMG) -total=200 -concorrencia=5 -alvos="$(ALVOS)" -cenario=cancelamento

simulador-motorista: up-local build-simulador ## 100 operacoes de motorista (leve)
	docker run --rm --cpus=1 --memory=512m --network $(REDE) $(SIM_IMG) -total=100 -concorrencia=2 -alvos="$(ALVOS)" -cenario=motorista

simulador-busca: up-local build-simulador ## 300 buscas de itinerarios (leve)
	docker run --rm --cpus=1 --memory=512m --network $(REDE) $(SIM_IMG) -total=300 -concorrencia=5 -alvos="$(ALVOS)" -cenario=busca

simulador: simulador-contencao simulador-cancelamento simulador-motorista simulador-busca ## roda os 4 cenarios leves em sequencia (carga reduzida)

simulador-todos: simulador ## alias de simulador

down-a: ## para a empresa A (simula a queda)
	$(COMPOSE_LOCAL) stop empresa-a

down-b: ## para a empresa B (simula a queda)
	$(COMPOSE_LOCAL) stop empresa-b

down-c: ## para a empresa C (simula a queda)
	$(COMPOSE_LOCAL) stop empresa-c

up-a: ## religa a empresa A (recuperacao)
	$(COMPOSE_LOCAL) start empresa-a

up-b: ## religa a empresa B (recuperacao)
	$(COMPOSE_LOCAL) start empresa-b

up-c: ## religa a empresa C (recuperacao)
	$(COMPOSE_LOCAL) start empresa-c

clean: ## remove binarios e contêineres
	rm -rf $(BIN)
	$(COMPOSE_LOCAL) down -v

clean-data: ## apaga os dados das empresas (dados/*) mantendo a configuracao
	$(COMPOSE_LOCAL) down -v
	docker run --rm -v $(PWD)/dados:/dados alpine sh -c "rm -f /dados/*/*.log /dados/*/*.json /dados/*/reservas.json /dados/*/usuarios.json"