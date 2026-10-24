package simulador

/*
O QUE ESTÁ FAZENDO:
1. Fixa a rota de teste para todos os trabalhadores (ex: SSA -> CLE).
2. Força dezenas de goroutines concorrentes a tentarem reservar simultaneamente
   os mesmos trechos.
3. Estressa o Two-Phase Commit (2PC), a fase Prepare e a resolução de concorrência.

RESULTADOS ESPERADOS:
- Confirmação exata da capacidade máxima de assentos disponíveis (COMMITS).
- Rejeição segura com GLOBAL ABORT para todas as requisições excedentes.
- ZERO OVERBOOKING na auditoria pós-teste: nenhum assento a mais é vendido.
*/

import (
	"sync/atomic"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
)

type CenarioContencao struct{}

func (c *CenarioContencao) Nome() string {
	return "Alta Contenção (Disputa Concorrente por Vaga Única no 2PC)"
}

func (c *CenarioContencao) ExecutarPasso(ctx *ContextoExecucao) {
	const origemFixa = "SSA"
	const destinoFixo = "CLE"

	t0 := time.Now()
	var buscaRes cliente_servidor.RespostaBuscarItinerarios
	errBusca := ctx.Cliente.Executar(map[string]any{
		"tipo":    cliente_servidor.TipoBuscarItinerariosReq,
		"origem":  origemFixa,
		"destino": destinoFixo,
		"data":    "2026-10-30",
	}, cliente_servidor.TipoBuscarItinerariosRes, &buscaRes)

	atomic.AddInt64(&ctx.Stats.TotalOperacoes, 1)
	ctx.Stats.LatenciasBusca.Registrar(time.Since(t0))

	if errBusca != nil || len(buscaRes.Itinerarios) == 0 {
		atomic.AddInt64(&ctx.Stats.BuscasVazias, 1)
		return
	}
	atomic.AddInt64(&ctx.Stats.BuscasSucesso, 1)

	escolhido := buscaRes.Itinerarios[0]

	t1 := time.Now()
	var resRes cliente_servidor.RespostaReservar
	errRes := ctx.Cliente.Executar(map[string]any{
		"tipo":       cliente_servidor.TipoReservarItinerarioReq,
		"passageiro": ctx.WorkerNome,
		"itinerario": escolhido,
	}, cliente_servidor.TipoReservarItinerarioRes, &resRes)

	atomic.AddInt64(&ctx.Stats.TotalOperacoes, 1)
	ctx.Stats.LatenciasReserva.Registrar(time.Since(t1))

	if errRes != nil {
		atomic.AddInt64(&ctx.Stats.ErrosConexao, 1)
		atomic.AddInt64(&ctx.Stats.ReservasFalha, 1)
		return
	}

	if resRes.Sucesso {
		atomic.AddInt64(&ctx.Stats.ReservasConfirmadas, 1)
		if resRes.ReservaID > 0 {
			*ctx.ReservasLocais = append(*ctx.ReservasLocais, resRes.ReservaID)
		}
	} else {
		atomic.AddInt64(&ctx.Stats.ReservasRecusadas, 1)
	}
}
