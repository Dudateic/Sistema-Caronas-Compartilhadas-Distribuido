COMPOSE ?= docker compose
BIN     ?= bin

.DEFAULT_GOAL := help

.PHONY: help build test \
        servidor-a servidor-b servidor-c \
        up down restart logs \
        cliente-passageiro-a cliente-passageiro-b cliente-passageiro-c \
        cliente-motorista-a cliente-motorista-b cliente-motorista-c \
        down-a down-b down-c up-a up-b up-c \
        clean clean-data

help: ## Lista os comandos
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}'

build:
	@mkdir -p $(BIN)
	go build -o $(BIN)/servidor   ./aplicacao/servidor
	go build -o $(BIN)/motorista  ./aplicacao/motorista
	go build -o $(BIN)/passageiro ./aplicacao/passageiro

test:
	go test ./... -count=1

servidor-a:  ## TCP 6060 HTTP 9091
	$(COMPOSE) up --build empresa-a

servidor-b: ## TCP 7070 HTTP 9092
	$(COMPOSE) up --build empresa-b

servidor-c: ## TCP 8080 HTTP 9093
	$(COMPOSE) up --build empresa-c

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f --tail=100

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

clean:
	rm -rf $(BIN)
	$(COMPOSE) down -v

clean-data:
	docker run --rm -v "$(PWD):/work" -w /work alpine rm -rf dados