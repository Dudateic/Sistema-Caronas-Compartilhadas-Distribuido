COMPOSE ?= docker compose -f docker/docker-compose.yaml
BIN     ?= bin

.DEFAULT_GOAL := help

.PHONY: help build test \
        servidor-a servidor-b servidor-c \
        up down restart logs \
        cliente-passageiro-a cliente-passageiro-b cliente-passageiro-c \
        cliente-motorista-a cliente-motorista-b cliente-motorista-c \
        simulador \
        down-a down-b down-c up-a up-b up-c \
        clean clean-data

help: ## Lista os comandos disponíveis no Makefile
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}'

build: ## Compila todos os binários localmente (servidor, clientes e simulador)
	@mkdir -p $(BIN)
	go build -o $(BIN)/servidor   ./aplicacao/servidor
	go build -o $(BIN)/motorista  ./aplicacao/motorista
	go build -o $(BIN)/passageiro ./aplicacao/passageiro
	go build -o $(BIN)/simulador  ./aplicacao/simulador

test: ## Executa todos os testes automatizados com cobertura
	go test ./tests/... -v -count=1

up: ## Sobe todos os 3 servidores do consórcio em segundo plano
	$(COMPOSE) up -d --build empresa-a empresa-b empresa-c

down: ## Encerra todos os contêineres do consórcio
	$(COMPOSE) down

restart: down up ## Reinicia todos os servidores do consórcio

logs: ## Exibe logs de todos os servidores em tempo real
	$(COMPOSE) logs -f --tail=100

servidor-a: ## Sobe apenas o servidor da Empresa A (TCP 6060, HTTP 9091)
	$(COMPOSE) up --build empresa-a

servidor-b: ## Sobe apenas o servidor da Empresa B (TCP 7070, HTTP 9092)
	$(COMPOSE) up --build empresa-b

servidor-c: ## Sobe apenas o servidor da Empresa C (TCP 8080, HTTP 9093)
	$(COMPOSE) up --build empresa-c

cliente-passageiro-a: ## Abre terminal interativo do passageiro conectado à Empresa A
	$(COMPOSE) run --rm passageiro-a

cliente-passageiro-b: ## Abre terminal interativo do passageiro conectado à Empresa B
	$(COMPOSE) run --rm passageiro-b

cliente-passageiro-c: ## Abre terminal interativo do passageiro conectado à Empresa C
	$(COMPOSE) run --rm passageiro-c

cliente-motorista-a: ## Abre terminal interativo do motorista conectado à Empresa A
	$(COMPOSE) run --rm motorista-a

cliente-motorista-b: ## Abre terminal interativo do motorista conectado à Empresa B
	$(COMPOSE) run --rm motorista-b

cliente-motorista-c: ## Abre terminal interativo do motorista conectado à Empresa C
	$(COMPOSE) run --rm motorista-c

simulador: ## Executa simulador de alta concorrência e carga no consórcio
	$(COMPOSE) run --rm simulador

down-a: ## Derruba o nó da Empresa A (injeção de falha)
	$(COMPOSE) stop empresa-a

down-b: ## Derruba o nó da Empresa B (injeção de falha)
	$(COMPOSE) stop empresa-b

down-c: ## Derruba o nó da Empresa C (injeção de falha)
	$(COMPOSE) stop empresa-c

up-a: ## Recupera o nó da Empresa A (validação de recuperação de estado)
	$(COMPOSE) start empresa-a

up-b: ## Recupera o nó da Empresa B (validação de recuperação de estado)
	$(COMPOSE) start empresa-b

up-c: ## Recupera o nó da Empresa C (validação de recuperação de estado)
	$(COMPOSE) start empresa-c

clean: ## Remove binários compilados e contêineres
	rm -rf $(BIN)
	$(COMPOSE) down -v

clean-data: ## Limpa os volumes do Docker sem mexer em permissões locais
	$(COMPOSE) down -v