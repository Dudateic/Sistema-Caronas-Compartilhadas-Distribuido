package simulador

/*
O QUE ESTÁ FAZENDO:
1. Coleta durações de execução de forma thread-safe (com sync/atomic e RWMutex).
2. Segmenta as latências por categoria operacional (Buscas vs. Reservas 2PC vs. Cancelamentos).
3. Calcula métricas estatísticas essenciais:
   - P50 (Mediana): comportamento padrão da maioria das requisições.
   - P95: cauda operacional relevante para SLAs distribuídos.
   - P99: pior caso observado sob contenção ou disputa concorrente.
   - Média aritmética global.

RESULTADOS ESPERADOS:
- Medições isoladas: buscas leves não camuflam o custo transacional do 2PC.
- Ausência de gargalo de sincronização de memória entre goroutines concorrentes.
*/

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type GravadorLatencia struct {
	mu        sync.Mutex
	latencias []int64
	totalMs   int64
	contagem  int64
}

func (g *GravadorLatencia) Registrar(duracao time.Duration) {
	ms := duracao.Milliseconds()
	atomic.AddInt64(&g.totalMs, ms)
	atomic.AddInt64(&g.contagem, 1)

	g.mu.Lock()
	if len(g.latencias) < 150000 {
		g.latencias = append(g.latencias, ms)
	}
	g.mu.Unlock()
}

func (g *GravadorLatencia) Percentis() (p50, p95, p99 int64, media float64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	n := len(g.latencias)
	if n == 0 {
		return 0, 0, 0, 0
	}

	sort.Slice(g.latencias, func(i, j int) bool {
		return g.latencias[i] < g.latencias[j]
	})

	calc := func(p float64) int64 {
		idx := int(float64(n) * p)
		if idx >= n {
			idx = n - 1
		}
		return g.latencias[idx]
	}

	tot := atomic.LoadInt64(&g.totalMs)
	cnt := atomic.LoadInt64(&g.contagem)
	med := 0.0
	if cnt > 0 {
		med = float64(tot) / float64(cnt)
	}

	return calc(0.50), calc(0.95), calc(0.99), med
}

type EstatisticasConsorcio struct {
	TotalOperacoes      int64
	BuscasSucesso       int64
	BuscasVazias        int64
	BuscasFalha         int64
	ReservasConfirmadas int64
	ReservasRecusadas   int64
	ReservasFalha       int64
	CancelamentosOK     int64
	CancelamentosFalha  int64
	ErrosConexao        int64

	LatenciasBusca   GravadorLatencia
	LatenciasReserva GravadorLatencia
	LatenciasCancela GravadorLatencia
}
