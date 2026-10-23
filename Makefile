COMPOSE ?= docker compose -f docker/docker-compose.yaml
BIN     ?= bin

.DEFAULT_GOAL := help

.PHONY: help build test \
        servidor-a servidor-b servidor-c \
        run-servidor-a run-servidor-b run-servidor-c \
        up down restart logs \
        cliente-passageiro-a cliente-passageiro-b cliente-passageiro-c \
        cliente-motorista-a cliente-motorista-b cliente-motorista-c \
        simulador \
        down-a down-b down-c up-a up-b up-c \
        clean clean-data

help:
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}'

build:
	@mkdir -p $(BIN)
	go build -o $(BIN)/servidor   ./aplicacao/servidor
	go build -o $(BIN)/motorista  ./aplicacao/motorista
	go build -o $(BIN)/passageiro ./aplicacao/passageiro
	go build -o $(BIN)/simulador  ./aplicacao/simulador

test:
	go test ./tests/... -v -count=1


up:
	$(COMPOSE) up -d --build empresa-a empresa-b empresa-c

down:
	$(COMPOSE) down

restart: down up

logs:
	$(COMPOSE) logs -f --tail=100

servidor-a: ## TCP 6060, HTTP 9091
	$(COMPOSE) up --build empresa-a

servidor-b: ## TCP 7070, HTTP 9092
	$(COMPOSE) up --build empresa-b

servidor-c: ## TCP 8080, HTTP 9093
	$(COMPOSE) up --build empresa-c

run-servidor-a: build
	@mkdir -p ./dados/empresa_A
	./$(BIN)/servidor -empresa=A -tcp=:6060 -http=:9091 -dados=./dados/empresa_A -pares="B=http://localhost:9092,C=http://localhost:9093"

run-servidor-b: build
	@mkdir -p ./dados/empresa_B
	./$(BIN)/servidor -empresa=B -tcp=:7070 -http=:9092 -dados=./dados/empresa_B -pares="A=http://localhost:9091,C=http://localhost:9093"

run-servidor-c: build
	@mkdir -p ./dados/empresa_C
	./$(BIN)/servidor -empresa=C -tcp=:8080 -http=:9093 -dados=./dados/empresa_C -pares="A=http://localhost:9091,B=http://localhost:9092"

cliente-passageiro-a:
	$(COMPOSE) run --rm passageiro-a

cliente-passageiro-b:
	$(COMPOSE) run --rm passageiro-b

cliente-passageiro-c:
	$(COMPOSE) run --rm passageiro-c

cliente-motorista-a:
	$(COMPOSE) run --rm motorista-a

cliente-motorista-b:
	$(COMPOSE) run --rm motorista-b

cliente-motorista-c:
	$(COMPOSE) run --rm motorista-c

simulador:
	$(COMPOSE) run --rm simulador

down-a:
	$(COMPOSE) stop empresa-a

down-b:
	$(COMPOSE) stop empresa-b

down-c:
	$(COMPOSE) stop empresa-c

up-a:
	$(COMPOSE) start empresa-a

up-b:
	$(COMPOSE) start empresa-b

up-c:
	$(COMPOSE) start empresa-c

clean: ## Remove binários compilados e desliga contêineres Docker
	rm -rf $(BIN)
	$(COMPOSE) down -v

clean-data:
	docker compose -f docker/docker-compose.yaml down -v
	docker run --rm -v $(PWD)/dados:/dados alpine sh -c "rm -f /dados/*/*.log /dados/*/*.json /dados/*/reservas.json /dados/*/usuarios.json"