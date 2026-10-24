package simulador

/*
O QUE ESTÁ FAZENDO:
1. Simula o tráfego típico de um ambiente de produção real.
2. Cada trabalhador pesquisa rotas aleatórias e seleciona um dos itinerários
   retornados para efetivar a reserva.
3. Cobre itinerários diretos (locais) e com conexões entre empresas parceiras.

RESULTADOS ESPERADOS:
- Elevado índice de COMMITS em trechos com vagas disponíveis.
- Transição segura para ABORT quando trechos intermediários se esgotam.
- Nenhum itinerário confirmado parcialmente em servidores parceiros.
*/

import (
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
)

type CenarioMisto struct{}

func (c *CenarioMisto) Nome() string {
	return "Fluxo Realista Misto (Busca e Reserva Atômica 2PC)"
}

func (c *CenarioMisto) ExecutarPasso(ctx *ContextoExecucao) {
	origem := strings.TrimSpace(ctx.Origens[rand.Intn(len(ctx.Origens))])
	destino := strings.TrimSpace(ctx.Destinos[rand.Intn(len(ctx.Destinos))])
	for strings.EqualFold(origem, destino) {
		destino = strings.TrimSpace(ctx.Destinos[rand.Intn(len(ctx.Destinos))])
	}

	t0 := time.Now()
	var buscaRes cliente_servidor.RespostaBuscarItinerarios
	errBusca := ctx.Cliente.Executar(map[string]any{
		"tipo":    cliente_servidor.TipoBuscarItinerariosReq,
		"origem":  origem,
		"destino": destino,
		"data":    "2026-10-30",
	}, cliente_servidor.TipoBuscarItinerariosRes, &buscaRes)

	atomic.AddInt64(&ctx.Stats.TotalOperacoes, 1)
	ctx.Stats.LatenciasBusca.Registrar(time.Since(t0))

	if errBusca != nil || len(buscaRes.Itinerarios) == 0 {
		atomic.AddInt64(&ctx.Stats.BuscasVazias, 1)
		return
	}
	atomic.AddInt64(&ctx.Stats.BuscasSucesso, 1)

	escolhido := buscaRes.Itinerarios[rand.Intn(len(buscaRes.Itinerarios))]
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
