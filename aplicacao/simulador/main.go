package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

type Estatisticas struct {
	TotalRequisicoes    int64
	BuscasSucesso       int64
	BuscasVazias        int64
	BuscasFalha         int64
	ReservasConfirmadas int64
	ReservasRecusadas   int64
	ReservasFalha       int64
	ErrosConexao        int64
	LatenciasTotalMs    int64
	muLatencias         sync.Mutex
	LatenciasMs         []int64
}

func (e *Estatisticas) RegistrarLatencia(duracao time.Duration) {
	ms := duracao.Milliseconds()
	atomic.AddInt64(&e.LatenciasTotalMs, ms)
	e.muLatencias.Lock()
	if len(e.LatenciasMs) < 100000 {
		e.LatenciasMs = append(e.LatenciasMs, ms)
	}
	e.muLatencias.Unlock()
}

type ClienteTCPPool struct {
	alvo       string
	timeoutReq time.Duration
}

func (c *ClienteTCPPool) Executar(req any, tipoEsperado string, destino any) error {
	conn, err := net.DialTimeout("tcp", c.alvo, 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	dados, err := json.Marshal(req)
	if err != nil {
		return err
	}
	dados = append(dados, '\n')

	_ = conn.SetDeadline(time.Now().Add(c.timeoutReq))
	if _, err := conn.Write(dados); err != nil {
		return err
	}

	leitor := bufio.NewReader(conn)
	for {
		linha, err := leitor.ReadBytes('\n')
		if err != nil {
			return err
		}

		var base struct {
			Tipo string `json:"tipo"`
		}
		if err := json.Unmarshal(linha, &base); err != nil {
			continue
		}

		if base.Tipo == tipoEsperado {
			return json.Unmarshal(linha, destino)
		}
	}
}

func calcularPercentil(latencias []int64, p float64) int64 {
	if len(latencias) == 0 {
		return 0
	}
	idx := int(float64(len(latencias)) * p)
	if idx >= len(latencias) {
		idx = len(latencias) - 1
	}
	return latencias[idx]
}

// semearCaronas garante caronas e vagas ativas no consórcio para disputa concorrente
func semearCaronas(alvos []string, timeout time.Duration) {
	rotasIniciais := [][]string{
		{"SSA", "VIT", "CLE"},
		{"SSA", "FSA", "VIT"},
		{"FSA", "VIT", "CLE"},
		{"SSA", "CLE"},
	}

	for i, alvo := range alvos {
		cli := &ClienteTCPPool{alvo: alvo, timeoutReq: timeout}
		motorista := fmt.Sprintf("motorista_seed_%d", i+1)

		_ = cli.Executar(map[string]any{
			"tipo":         cliente_servidor.TipoCadastroReq,
			"nome":         motorista,
			"login":        motorista,
			"senha":        "123456",
			"tipo_usuario": string(modelos.UsuarioMotorista),
		}, cliente_servidor.TipoCadastroRes, &cliente_servidor.RespostaSimples{})

		for _, rota := range rotasIniciais {
			pubReq := map[string]any{
				"tipo":             cliente_servidor.TipoPublicarCaronaReq,
				"motorista":        motorista,
				"rota":             rota,
				"data":             "2026-10-30",
				"horario":          "12:00",
				"assentos_totais":  50,
				"preco_por_trecho": 15.0,
			}
			var pubRes cliente_servidor.RespostaPublicarCarona
			_ = cli.Executar(pubReq, cliente_servidor.TipoPublicarCaronaRes, &pubRes)
		}
	}
}

func main() {
	alvoPadrao := os.Getenv("ALVO")
	if alvoPadrao == "" {
		alvoPadrao = "empresa-a:6060"
	}

	totalFlag := flag.Int("total", 10000, "Volume total de operações de teste")
	concorrenciaFlag := flag.Int("concorrencia", 100, "Número de trabalhadores concorrentes (goroutines)")
	alvosFlag := flag.String("alvos", alvoPadrao, "Endereços TCP dos servidores (ex: empresa-a:6060)")
	alvoAliasFlag := flag.String("alvo", "", "Alias compatível para endereço alvo")
	timeoutFlag := flag.Int("timeout", 5, "Timeout em segundos por requisição")
	cenarioFlag := flag.String("cenario", "misto", "Cenário de teste: misto | contencao | busca")
	origensFlag := flag.String("origens", "SSA,FSA,VIT", "Cidades de origem")
	destinosFlag := flag.String("destinos", "VIT,CLE,FSA", "Cidades de destino")
	flag.Parse()

	alvoStr := *alvosFlag
	if *alvoAliasFlag != "" {
		alvoStr = *alvoAliasFlag
	}

	rawAlvos := strings.Split(alvoStr, ",")
	var alvos []string
	for _, a := range rawAlvos {
		limpo := strings.TrimSpace(a)
		limpo = strings.TrimPrefix(limpo, "http://")
		limpo = strings.TrimPrefix(limpo, "https://")
		if limpo != "" {
			alvos = append(alvos, limpo)
		}
	}
	if len(alvos) == 0 {
		alvos = []string{"empresa-a:6060"}
	}

	origens := strings.Split(*origensFlag, ",")
	destinos := strings.Split(*destinosFlag, ",")

	fmt.Println("==================================================================")
	fmt.Printf("   VAIJUNTO - TESTE DE CARGA DISTRIBUÍDA (%d REQUISIÇÕES)\n", *totalFlag)
	fmt.Println("==================================================================")
	fmt.Printf(" Trabalhadores paralelos : %d\n", *concorrenciaFlag)
	fmt.Printf(" Servidores alvo (TCP)   : %v\n", alvos)
	fmt.Printf(" Cenário                 : %s\n", strings.ToUpper(*cenarioFlag))
	fmt.Printf(" Origens / Destinos      : %v -> %v\n", origens, destinos)
	fmt.Println("==================================================================")

	timeoutDur := time.Duration(*timeoutFlag) * time.Second

	fmt.Println("Garantindo caronas ativas no consórcio para teste de concorrência...")
	semearCaronas(alvos, timeoutDur)

	fmt.Println("Disparando requisições paralelas e reservas atômicas (2PC)...")

	var stats Estatisticas
	inicioGeral := time.Now()

	tarefas := make(chan int, *totalFlag)
	for i := 0; i < *totalFlag; i++ {
		tarefas <- i
	}
	close(tarefas)

	var wg sync.WaitGroup
	for w := 0; w < *concorrenciaFlag; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			nomeUsuario := fmt.Sprintf("sim_worker_%d", workerID)
			alvo := alvos[workerID%len(alvos)]
			cliente := &ClienteTCPPool{alvo: alvo, timeoutReq: timeoutDur}

			cadReq := map[string]any{
				"tipo":         cliente_servidor.TipoCadastroReq,
				"nome":         nomeUsuario,
				"login":        nomeUsuario,
				"senha":        "123456",
				"tipo_usuario": string(modelos.UsuarioPassageiro),
			}
			var cadRes cliente_servidor.RespostaSimples
			_ = cliente.Executar(cadReq, cliente_servidor.TipoCadastroRes, &cadRes)

			for range tarefas {
				origem := strings.TrimSpace(origens[rand.Intn(len(origens))])
				destino := strings.TrimSpace(destinos[rand.Intn(len(destinos))])
				for strings.EqualFold(origem, destino) {
					destino = strings.TrimSpace(destinos[rand.Intn(len(destinos))])
				}

				if *cenarioFlag == "contencao" {
					origem = "SSA"
					destino = "CLE"
				}

				// 1. Busca de itinerários
				t0 := time.Now()
				buscaReq := map[string]any{
					"tipo":    cliente_servidor.TipoBuscarItinerariosReq,
					"origem":  origem,
					"destino": destino,
				}
				var buscaRes cliente_servidor.RespostaBuscarItinerarios
				errBusca := cliente.Executar(buscaReq, cliente_servidor.TipoBuscarItinerariosRes, &buscaRes)
				durBusca := time.Since(t0)

				atomic.AddInt64(&stats.TotalRequisicoes, 1)
				stats.RegistrarLatencia(durBusca)

				if errBusca != nil {
					atomic.AddInt64(&stats.ErrosConexao, 1)
					atomic.AddInt64(&stats.BuscasFalha, 1)
					continue
				}

				if !buscaRes.Sucesso {
					atomic.AddInt64(&stats.BuscasFalha, 1)
					continue
				}

				if len(buscaRes.Itinerarios) == 0 {
					atomic.AddInt64(&stats.BuscasVazias, 1)
				} else {
					atomic.AddInt64(&stats.BuscasSucesso, 1)
				}

				// 2. Reserva concorrente via 2PC
				if *cenarioFlag != "busca" && len(buscaRes.Itinerarios) > 0 {
					escolhido := buscaRes.Itinerarios[rand.Intn(len(buscaRes.Itinerarios))]

					t1 := time.Now()
					resReq := map[string]any{
						"tipo":       cliente_servidor.TipoReservarItinerarioReq,
						"passageiro": nomeUsuario,
						"itinerario": escolhido,
					}
					var resRes cliente_servidor.RespostaReservar
					errRes := cliente.Executar(resReq, cliente_servidor.TipoReservarItinerarioRes, &resRes)
					durRes := time.Since(t1)

					atomic.AddInt64(&stats.TotalRequisicoes, 1)
					stats.RegistrarLatencia(durRes)

					if errRes != nil {
						atomic.AddInt64(&stats.ErrosConexao, 1)
						atomic.AddInt64(&stats.ReservasFalha, 1)
					} else if resRes.Sucesso {
						atomic.AddInt64(&stats.ReservasConfirmadas, 1)
					} else {
						atomic.AddInt64(&stats.ReservasRecusadas, 1)
					}
				}
			}
		}(w)
	}

	wg.Wait()
	tempoTotal := time.Since(inicioGeral)

	stats.muLatencias.Lock()
	sort.Slice(stats.LatenciasMs, func(i, j int) bool {
		return stats.LatenciasMs[i] < stats.LatenciasMs[j]
	})
	p50 := calcularPercentil(stats.LatenciasMs, 0.50)
	p95 := calcularPercentil(stats.LatenciasMs, 0.95)
	p99 := calcularPercentil(stats.LatenciasMs, 0.99)
	stats.muLatencias.Unlock()

	totalReqs := atomic.LoadInt64(&stats.TotalRequisicoes)
	rps := 0.0
	if tempoTotal.Seconds() > 0 {
		rps = float64(totalReqs) / tempoTotal.Seconds()
	}

	buscasTotal := atomic.LoadInt64(&stats.BuscasSucesso) + atomic.LoadInt64(&stats.BuscasVazias) + atomic.LoadInt64(&stats.BuscasFalha)
	reservasTotal := atomic.LoadInt64(&stats.ReservasConfirmadas) + atomic.LoadInt64(&stats.ReservasRecusadas) + atomic.LoadInt64(&stats.ReservasFalha)

	fmt.Println("\n==================================================================")
	fmt.Println("             RELATÓRIO DE ESTRESSE & ATOMICIDADE                  ")
	fmt.Println("==================================================================")
	fmt.Printf("Tempo de execução        : %v\n", tempoTotal.Round(time.Millisecond))
	fmt.Printf("Total de requisições     : %d\n", totalReqs)
	fmt.Printf("Vazão média (Throughput) : %.2f req/s\n", rps)
	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("Buscas enviadas          : %d\n", buscasTotal)
	fmt.Printf("  ├── Com rotas válidas  : %d\n", atomic.LoadInt64(&stats.BuscasSucesso))
	fmt.Printf("  └── Sem rotas livres   : %d\n", atomic.LoadInt64(&stats.BuscasVazias))
	fmt.Printf("Reservas tentadas (2PC)  : %d\n", reservasTotal)
	fmt.Printf("  ├── Confirmadas (COMMIT): %d\n", atomic.LoadInt64(&stats.ReservasConfirmadas))
	fmt.Printf("  └── Rejeitadas (ABORT)  : %d (Concorrência/Esgotamento)\n", atomic.LoadInt64(&stats.ReservasRecusadas))
	fmt.Printf("Falhas de Conexão        : %d\n", atomic.LoadInt64(&stats.ErrosConexao))
	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("Latência P50 (Mediana)   : %d ms\n", p50)
	fmt.Printf("Latência P95             : %d ms\n", p95)
	fmt.Printf("Latência P99             : %d ms\n", p99)
	fmt.Println("==================================================================")
}
