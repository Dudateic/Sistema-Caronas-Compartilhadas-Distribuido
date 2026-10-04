# Arquitetura Distribuída: Protocolo Two-Phase Commit (2PC)

> **TEC502 - TP02 - Concorrência e Conectividade — Semestre 2026.2 | UEFS**

## 1. Visão Geral do Protocolo

O protocolo Two-Phase Commit (2PC) funciona como um grande acordo entre os servidores para manter o sistema organizado. No sistema, a função dele é simples: garantir que uma reserva que passa por vários servidores diferentes seja feita por completo ou totalmente cancelada.

Na prática, se um passageiro quiser uma viagem com vários trechos controlados por parceiros distintos, a reserva só vale se der certo em todos eles. É a regra do "tudo ou nada". Isso mantém os dados corretos e seguros, sem precisar de um servidor central organizando tudo.

## 2. Fases do Protocolo e Endpoints REST

Toda a conversa para organizar as reservas entre os servidores parceiros acontece apenas via API REST. O servidor que recebe o pedido do passageiro vira o "coordenador" daquela reserva e lidera o processo em duas etapas bem claras.

### Fase 1: Preparação (Prepare)

Nesta primeira etapa de checagem, o coordenador manda um aviso ao mesmo tempo para todos os servidores que cuidam dos trechos da viagem, pedindo para eles segurarem a vaga temporariamente.

* **Endpoint:** `POST /api/v1/transacoes/prepare`
* **O que é enviado (`PedidoBloqueio`):** O pedido vai com os dados principais: qual é a carona, origem, destino, quem é o passageiro e um relógio lógico (`timestamp`). Esse relógio serve como regra de desempate caso duas pessoas tentem pegar a mesma vaga exatamente no mesmo instante.
* **Respostas Possíveis:**
* HTTP `200 OK`: O servidor parceiro confirma que tem vaga e bloqueia o assento temporariamente (Voto: SIM).
* HTTP `409 Conflict`: O servidor parceiro recusa o pedido, avisando que a vaga acabou ou que outra pessoa chegou primeiro (Voto: NÃO).



### Fase 2: Veredito (Commit ou Rollback)

A decisão final do coordenador depende apenas das respostas recebidas na primeira etapa.

#### Confirmação (Commit)

A viagem só é confirmada se **todos** os parceiros disserem "SIM". Quando todo mundo aprova, o coordenador manda a ordem final para garantir a vaga de vez.

* **Endpoint:** `POST /api/v1/transacoes/{tx_id}/commit`
* **O que é enviado (`PedidoCommit`):** É necessário mandar apenas o ID global da reserva (`tx_id`).
* **Resposta Esperada:** HTTP `200 OK`.

#### Cancelamento (Rollback / Abort)

O cancelamento acontece na hora se pelo menos um parceiro votar "NÃO" ou se a internet falhar e der tempo limite (*timeout*). O coordenador então manda todo mundo soltar as vagas que estavam presas.

* **Endpoint:** `POST /api/v1/transacoes/{tx_id}/rollback`
* **O que é enviado (`PedidoRollback`):** Manda o ID da reserva (`tx_id`) e, se quiser, um aviso explicando o motivo de ter dado errado.
* **Resposta Esperada:** HTTP `200 OK`.

## 3. Contratos de Dados (Implementação em Go)

Para fazer isso funcionar na prática, o sistema usa as estruturas de dados a seguir, que definem exatamente o formato das informações enviadas pela rede:

```go
package protocolo

type ErroHTTP struct {
    Status   int    `json:"status"`
    Mensagem string `json:"mensagem"`
}

type PedidoBloqueio struct {
    TxID       string `json:"tx_id"`
    CaronaID   int    `json:"carona_id"`
    Origem     string `json:"origem"`
    Destino    string `json:"destino"`
    Passageiro string `json:"passageiro"`
    Data       string `json:"data"`
    Timestamp  int64  `json:"timestamp"`
}

type PedidoCommit struct {
    TxID string `json:"tx_id"`
}

type PedidoRollback struct {
    TxID   string `json:"tx_id"`
    Motivo string `json:"motivo,omitempty"`
}

```