package simulador

/*

O QUE ESTÁ FAZENDO:
1. Consolida os contadores de throughput (vazão média por segundo).
2. Extrai os percentis P50, P95, P99 e médias de latência por categoria.
3. Imprime uma tabela clara e visualmente estruturada no terminal padrão.

RESULTADOS ESPERADOS:
- Apresentação transparente das métricas de desempenho para documentação e entrega.
- Discriminação nítida entre operações de leitura rápida e reservas atômicas 2PC.
*/

import (
	"fmt"
	"sync/atomic"
	"time"
)

func ExibirRelatorioFinal(stats *EstatisticasConsorcio, duracao time.Duration) {
	totalOps := atomic.LoadInt64(&stats.TotalOperacoes)
	vazao := 0.0
	if duracao.Seconds() > 0 {
		vazao = float64(totalOps) / duracao.Seconds()
	}

	p50B, p95B, p99B, medB := stats.LatenciasBusca.Percentis()
	p50R, p95R, p99R, medR := stats.LatenciasReserva.Percentis()
	p50C, p95C, p99C, medC := stats.LatenciasCancela.Percentis()

	fmt.Println("==================================================================")
	fmt.Println("             RELATÓRIO DE ESTRESSE & ATOMICIDADE (2PC)            ")
	fmt.Println("==================================================================")
	fmt.Printf("Tempo de execução        : %v", duracao.Round(time.Millisecond))
	fmt.Printf("Volume total executado   : %d operações", totalOps)
	fmt.Printf("Throughput (Vazão média) : %.2f ops/s", vazao)
	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("Buscas de itinerários    : %d sucesso | %d sem rota | %d falhas",
		atomic.LoadInt64(&stats.BuscasSucesso),
		atomic.LoadInt64(&stats.BuscasVazias),
		atomic.LoadInt64(&stats.BuscasFalha),
	)
	fmt.Printf("Reservas atômicas (2PC)  : %d confirmadas (COMMIT) | %d recusadas (ABORT)",
		atomic.LoadInt64(&stats.ReservasConfirmadas),
		atomic.LoadInt64(&stats.ReservasRecusadas),
	)
	if atomic.LoadInt64(&stats.CancelamentosOK)+atomic.LoadInt64(&stats.CancelamentosFalha) > 0 {
		fmt.Printf("Cancelamentos efetuados  : %d confirmados | %d falhas",
			atomic.LoadInt64(&stats.CancelamentosOK),
			atomic.LoadInt64(&stats.CancelamentosFalha),
		)
	}
	fmt.Printf("Falhas de conexão / drop : %d", atomic.LoadInt64(&stats.ErrosConexao))
	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("Latência Buscas (ms)     : P50=%dms | P95=%dms | P99=%dms | Média=%.1fms", p50B, p95B, p99B, medB)
	fmt.Printf("Latência Reservas 2PC    : P50=%dms | P95=%dms | P99=%dms | Média=%.1fms", p50R, p95R, p99R, medR)
	if atomic.LoadInt64(&stats.CancelamentosOK) > 0 {
		fmt.Printf("Latência Cancelamentos   : P50=%dms | P95=%dms | P99=%dms | Média=%.1fms", p50C, p95C, p99C, medC)
	}
	fmt.Println("==================================================================")
}
