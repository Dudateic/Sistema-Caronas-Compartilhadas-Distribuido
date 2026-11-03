package main

import (
	main2 "Sistema-Caronas-Compartilhadas-Distribuido/aplicacao/simulador"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

func main() {
	alvoPadrao := os.Getenv("ALVO")
	if alvoPadrao == "" {
		alvoPadrao = "empresa-a:6060,empresa-b:7070,empresa-c:8080"
	}

	totalFlag := flag.Int("total", 5000, "Volume total de operações de teste")
	concorrenciaFlag := flag.Int("concorrencia", 50, "Número de trabalhadores concorrentes (goroutines)")
	alvosFlag := flag.String("alvos", alvoPadrao, "Endereços TCP dos nós (separados por vírgula)")
	timeoutFlag := flag.Int("timeout", 5, "Timeout em segundos por requisição")
	cenarioFlag := flag.String("cenario", "misto", "Cenário: misto | contencao | busca | cancelamento | motorista")
	assentosFlag := flag.Int("assentos", 10, "Quantidade de assentos em cada carona semeada")
	origensFlag := flag.String("origens", "SSA,FSA,VIT", "Cidades de origem")
	destinosFlag := flag.String("destinos", "VIT,CLE,BRU", "Cidades de destino")
	flag.Parse()

	rawAlvos := strings.Split(*alvosFlag, ",")
	var alvos []string
	for _, a := range rawAlvos {
		limpo := strings.TrimSpace(a)
		limpo = strings.TrimPrefix(limpo, "http://")
		limpo = strings.TrimPrefix(limpo, "https://")
		if limpo != "" {
			alvos = append(alvos, limpo)
		}
	}

	timeoutDur := time.Duration(*timeoutFlag) * time.Second
	cenario := main2.ObterCenario(*cenarioFlag)

	fmt.Println("==================================================================")
	fmt.Printf(" Cenário selecionado     : %s", cenario.Nome())
	fmt.Printf(" Trabalhadores paralelos : %d goroutines", *concorrenciaFlag)
	fmt.Printf(" Nós do Consórcio        : %v", alvos)
	fmt.Println("==================================================================")

	main2.SemearCatalogoCaronas(alvos, timeoutDur, *assentosFlag)

	var stats main2.EstatisticasConsorcio
	inicioGeral := time.Now()

	tarefas := make(chan int, *totalFlag)
	for i := 0; i < *totalFlag; i++ {
		tarefas <- i
	}
	close(tarefas)

	origens := strings.Split(*origensFlag, ",")
	destinos := strings.Split(*destinosFlag, ",")

	var wg sync.WaitGroup
	for w := 0; w < *concorrenciaFlag; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			nomeUsuario := fmt.Sprintf("sim_worker_%d_%d", workerID, time.Now().UnixNano()%100000)
			alvo := alvos[workerID%len(alvos)]
			cli := main2.NovoClienteTCP(alvo, timeoutDur)
			defer cli.Fechar()

			_ = cli.Executar(map[string]any{
				"tipo":         cliente_servidor.TipoCadastroReq,
				"nome":         nomeUsuario,
				"login":        nomeUsuario,
				"senha":        "123456",
				"tipo_usuario": string(modelos.UsuarioPassageiro),
			}, cliente_servidor.TipoCadastroRes, &cliente_servidor.RespostaSimples{})

			var reservasLocais []int
			ctx := &main2.ContextoExecucao{
				Cliente:        cli,
				WorkerNome:     nomeUsuario,
				Origens:        origens,
				Destinos:       destinos,
				Stats:          &stats,
				ReservasLocais: &reservasLocais,
			}

			for range tarefas {
				cenario.ExecutarPasso(ctx)
			}
		}(w)
	}

	wg.Wait()
	tempoTotal := time.Since(inicioGeral)

	main2.ExibirRelatorioFinal(&stats, tempoTotal)
	main2.AuditarInvariantes(alvos, timeoutDur)
}
