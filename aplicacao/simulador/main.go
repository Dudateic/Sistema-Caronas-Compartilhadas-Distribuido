package simulador

/*
O QUE ESTÁ FAZENDO:
1. Faz o parse de todas as flags de linha de comando configuradas pelo usuário ou Docker.
2. Normaliza os alvos dos servidores (TCP) do consórcio distribuído.
3. Inicializa o catálogo de caronas ativas através da rotina de semeadura.
4. Spawna um pool de goroutines (trabalhadores concorrentes) e distribui as tarefas
   via canais seguros (channels).
5. Cada trabalhador cadastra-se como passageiro e executa repetidamente passos do
   cenário de teste selecionado.
6. Ao término, dispara a geração do relatório final de latências/throughput e aciona
   a auditoria de invariantes distribuídos.

RESULTADOS ESPERADOS:
- Conclusão de todas as requisições solicitadas sem deadlocks ou panics.
- Distribuição uniforme de carga entre os nós configurados.
- Relatório de encerramento impresso com estatísticas consolidadas.
- Chamada bem-sucedida do auditor pós-teste confirmando Zero Overbooking.
*/

import (
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
	cenario := ObterCenario(*cenarioFlag)

	fmt.Println("==================================================================")
	fmt.Printf("   VAIJUNTO - SIMULADOR DISTRIBUÍDO (%d REQUISIÇÕES)", *totalFlag)
	fmt.Println("==================================================================")
	fmt.Printf(" Cenário selecionado     : %s", cenario.Nome())
	fmt.Printf(" Trabalhadores paralelos : %d goroutines", *concorrenciaFlag)
	fmt.Printf(" Nós do Consórcio        : %v", alvos)
	fmt.Println("==================================================================")

	// Garante viagens cadastradas no consórcio para permitir disputas concorrentes
	SemearCatalogoCaronas(alvos, timeoutDur, *assentosFlag)

	var stats EstatisticasConsorcio
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
			cli := NovoClienteTCP(alvo, timeoutDur)
			defer cli.Fechar()

			// Cadastro único do passageiro simulado
			_ = cli.Executar(map[string]any{
				"tipo":         cliente_servidor.TipoCadastroReq,
				"nome":         nomeUsuario,
				"login":        nomeUsuario,
				"senha":        "123456",
				"tipo_usuario": string(modelos.UsuarioPassageiro),
			}, cliente_servidor.TipoCadastroRes, &cliente_servidor.RespostaSimples{})

			var reservasLocais []int
			ctx := &ContextoExecucao{
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

	// Exibe o painel consolidado com percentis de resposta e auditoria
	ExibirRelatorioFinal(&stats, tempoTotal)
	AuditarInvariantes(alvos, timeoutDur)
}
