package simulador

import (
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"
)

type CenarioResilienciaMotorista struct{}

func (c *CenarioResilienciaMotorista) Nome() string {
	return "Resiliência e Cancelamento de Caronas pelo Motorista"
}

func (c *CenarioResilienciaMotorista) ExecutarPasso(ctx *ContextoExecucao) {
	loginMotorista := fmt.Sprintf("mot_dinamico_%s", ctx.WorkerNome)

	_ = ctx.Cliente.Executar(map[string]any{
		"tipo":         cliente_servidor.TipoCadastroReq,
		"nome":         loginMotorista,
		"login":        loginMotorista,
		"senha":        "123456",
		"tipo_usuario": string(modelos.UsuarioMotorista),
	}, cliente_servidor.TipoCadastroRes, &cliente_servidor.RespostaSimples{})

	var pubRes cliente_servidor.RespostaPublicarCarona
	errPub := ctx.Cliente.Executar(map[string]any{
		"tipo":             cliente_servidor.TipoPublicarCaronaReq,
		"motorista":        loginMotorista,
		"rota":             []string{"SSA", "FSA", "CLE"},
		"data":             "2026-10-30",
		"horario":          "14:00",
		"assentos_totais":  5,
		"preco_por_trecho": 12.0,
	}, cliente_servidor.TipoPublicarCaronaRes, &pubRes)

	atomic.AddInt64(&ctx.Stats.TotalOperacoes, 1)

	if errPub != nil || !pubRes.Sucesso || pubRes.CaronaID == 0 {
		atomic.AddInt64(&ctx.Stats.BuscasFalha, 1)
		return
	}

	time.Sleep(time.Duration(15+rand.Intn(35)) * time.Millisecond)

	tCanc := time.Now()
	var cancRes cliente_servidor.RespostaSimples
	errCanc := ctx.Cliente.Executar(map[string]any{
		"tipo":      cliente_servidor.TipoCancelarCaronaReq,
		"carona_id": pubRes.CaronaID,
		"motorista": loginMotorista,
	}, cliente_servidor.TipoCancelarCaronaRes, &cancRes)

	atomic.AddInt64(&ctx.Stats.TotalOperacoes, 1)
	ctx.Stats.LatenciasCancela.Registrar(time.Since(tCanc))

	if errCanc == nil && cancRes.Sucesso {
		atomic.AddInt64(&ctx.Stats.CancelamentosOK, 1)
	} else {
		atomic.AddInt64(&ctx.Stats.CancelamentosFalha, 1)
	}
}
