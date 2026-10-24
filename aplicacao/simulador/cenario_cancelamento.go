package simulador

/*
O QUE ESTÁ FAZENDO:
1. Realiza buscas e reservas bem-sucedidas.
2. Mantém o registro dos bilhetes confirmados em memória local do worker.
3. Com probabilidade de 40%, dispara o cancelamento de um bilhete já adquirido.
4. Valida se o cancelamento local propaga para as empresas que detêm os trechos.

RESULTADOS ESPERADOS:
- Liberação imediata dos assentos cancelados nos servidores de dados.json.
- Reutilização instantânea desses assentos por novas buscas e reservas.
- Zero assentos perdidos ou inconsistentes no encerramento.
*/

import (
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
)

type CenarioCancelamento struct{}

func (c *CenarioCancelamento) Nome() string {
	return "Ciclo de Vida com Cancelamento de Reserva"
}

func (c *CenarioCancelamento) ExecutarPasso(ctx *ContextoExecucao) {
	if len(*ctx.ReservasLocais) > 0 && rand.Float64() < 0.40 {
		idReserva := (*ctx.ReservasLocais)[0]
		*ctx.ReservasLocais = (*ctx.ReservasLocais)[1:]

		tCanc := time.Now()
		var cancRes cliente_servidor.RespostaSimples
		errCanc := ctx.Cliente.Executar(map[string]any{
			"tipo":       cliente_servidor.TipoCancelarReservaReq,
			"reserva_id": idReserva,
			"passageiro": ctx.WorkerNome,
		}, cliente_servidor.TipoCancelarReservaRes, &cancRes)

		atomic.AddInt64(&ctx.Stats.TotalOperacoes, 1)
		ctx.Stats.LatenciasCancela.Registrar(time.Since(tCanc))

		if errCanc == nil && cancRes.Sucesso {
			atomic.AddInt64(&ctx.Stats.CancelamentosOK, 1)
		} else {
			atomic.AddInt64(&ctx.Stats.CancelamentosFalha, 1)
		}
		return
	}

	origem := strings.TrimSpace(ctx.Origens[rand.Intn(len(ctx.Origens))])
	destino := strings.TrimSpace(ctx.Destinos[rand.Intn(len(ctx.Destinos))])
	for strings.EqualFold(origem, destino) {
		destino = strings.TrimSpace(ctx.Destinos[rand.Intn(len(ctx.Destinos))])
	}

	var buscaRes cliente_servidor.RespostaBuscarItinerarios
	_ = ctx.Cliente.Executar(map[string]any{
		"tipo":    cliente_servidor.TipoBuscarItinerariosReq,
		"origem":  origem,
		"destino": destino,
		"data":    "2026-10-30",
	}, cliente_servidor.TipoBuscarItinerariosRes, &buscaRes)

	atomic.AddInt64(&ctx.Stats.TotalOperacoes, 1)

	if len(buscaRes.Itinerarios) == 0 {
		atomic.AddInt64(&ctx.Stats.BuscasVazias, 1)
		return
	}
	atomic.AddInt64(&ctx.Stats.BuscasSucesso, 1)

	escolhido := buscaRes.Itinerarios[rand.Intn(len(buscaRes.Itinerarios))]
	var resRes cliente_servidor.RespostaReservar
	_ = ctx.Cliente.Executar(map[string]any{
		"tipo":       cliente_servidor.TipoReservarItinerarioReq,
		"passageiro": ctx.WorkerNome,
		"itinerario": escolhido,
	}, cliente_servidor.TipoReservarItinerarioRes, &resRes)

	atomic.AddInt64(&ctx.Stats.TotalOperacoes, 1)

	if resRes.Sucesso {
		atomic.AddInt64(&ctx.Stats.ReservasConfirmadas, 1)
		if resRes.ReservaID > 0 {
			*ctx.ReservasLocais = append(*ctx.ReservasLocais, resRes.ReservaID)
		}
	} else {
		atomic.AddInt64(&ctx.Stats.ReservasRecusadas, 1)
	}
}
