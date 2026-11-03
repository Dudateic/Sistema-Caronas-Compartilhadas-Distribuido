package simulador

import (
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
)

type CenarioBusca struct{}

func (c *CenarioBusca) Nome() string {
	return "Leitura Intensiva (Busca DFS de Itinerários)"
}

func (c *CenarioBusca) ExecutarPasso(ctx *ContextoExecucao) {
	origem := strings.TrimSpace(ctx.Origens[rand.Intn(len(ctx.Origens))])
	destino := strings.TrimSpace(ctx.Destinos[rand.Intn(len(ctx.Destinos))])
	for strings.EqualFold(origem, destino) {
		destino = strings.TrimSpace(ctx.Destinos[rand.Intn(len(ctx.Destinos))])
	}

	t0 := time.Now()
	var buscaRes cliente_servidor.RespostaBuscarItinerarios
	err := ctx.Cliente.Executar(map[string]any{
		"tipo":            cliente_servidor.TipoBuscarItinerariosReq,
		"origem":          origem,
		"destino":         destino,
		"data":            "2026-10-30",
		"max_itinerarios": LimiteItinerarios,
		"max_trechos":     LimiteTrechos,
	}, cliente_servidor.TipoBuscarItinerariosRes, &buscaRes)
	duracao := time.Since(t0)

	atomic.AddInt64(&ctx.Stats.TotalOperacoes, 1)
	ctx.Stats.LatenciasBusca.Registrar(duracao)

	if err != nil {
		atomic.AddInt64(&ctx.Stats.ErrosConexao, 1)
		atomic.AddInt64(&ctx.Stats.BuscasFalha, 1)
		return
	}

	if len(buscaRes.Itinerarios) == 0 {
		atomic.AddInt64(&ctx.Stats.BuscasVazias, 1)
		return
	}
	atomic.AddInt64(&ctx.Stats.BuscasSucesso, 1)
}
