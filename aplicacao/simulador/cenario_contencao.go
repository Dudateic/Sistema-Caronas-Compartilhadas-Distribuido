package simulador

import (
	"sync"
	"sync/atomic"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
)

var muReservas sync.Mutex

var (
	onceBusca        sync.Once
	itinerarioPadrao any
	buscaValida      bool
)

type CenarioContencao struct{}

func (c *CenarioContencao) Nome() string {
	return "Alta Contenção (Disputa Concorrente por Vaga Única no 2PC)"
}

func (c *CenarioContencao) ExecutarPasso(ctx *ContextoExecucao) {
	const origemFixa = "SSA"
	const destinoFixo = "CLE"
	const dataFixa = "2026-10-30"

	onceBusca.Do(func() {
		t0 := time.Now()
		var buscaRes cliente_servidor.RespostaBuscarItinerarios
		errBusca := ctx.Cliente.Executar(map[string]any{
			"tipo":    cliente_servidor.TipoBuscarItinerariosReq,
			"origem":  origemFixa,
			"destino": destinoFixo,
			"data":    dataFixa,
		}, cliente_servidor.TipoBuscarItinerariosRes, &buscaRes)

		atomic.AddInt64(&ctx.Stats.TotalOperacoes, 1)
		ctx.Stats.LatenciasBusca.Registrar(time.Since(t0))

		if errBusca == nil && len(buscaRes.Itinerarios) > 0 {
			itinerarioPadrao = buscaRes.Itinerarios[0]
			buscaValida = true
			atomic.AddInt64(&ctx.Stats.BuscasSucesso, 1)
		} else {
			atomic.AddInt64(&ctx.Stats.BuscasVazias, 1)
		}
	})

	if !buscaValida || itinerarioPadrao == nil {
		return
	}

	t1 := time.Now()
	var resRes cliente_servidor.RespostaReservar
	errRes := ctx.Cliente.Executar(map[string]any{
		"tipo":       cliente_servidor.TipoReservarItinerarioReq,
		"passageiro": ctx.WorkerNome,
		"itinerario": itinerarioPadrao,
	}, cliente_servidor.TipoReservarItinerarioRes, &resRes)

	atomic.AddInt64(&ctx.Stats.TotalOperacoes, 1)
	ctx.Stats.LatenciasReserva.Registrar(time.Since(t1))

	if errRes != nil {
		atomic.AddInt64(&ctx.Stats.ErrosConexao, 1)
		atomic.AddInt64(&ctx.Stats.ReservasFalha, 1)
		time.Sleep(10 * time.Millisecond)
		return
	}

	if resRes.Sucesso {
		atomic.AddInt64(&ctx.Stats.ReservasConfirmadas, 1)
		if resRes.ReservaID > 0 {
			muReservas.Lock()
			*ctx.ReservasLocais = append(*ctx.ReservasLocais, resRes.ReservaID)
			muReservas.Unlock()
		}
	} else {
		atomic.AddInt64(&ctx.Stats.ReservasRecusadas, 1)
	}
}
