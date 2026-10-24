package simulador

/*
O QUE ESTÁ FAZENDO:
1. Define a struct `ContextoExecucao`, que encapsula cliente, estatísticas e estado
   local do worker sem vazamento de escopo.
2. Define a interface `CenarioTeste` que todo cenário deve implementar.
3. Fornece a função Factory `ObterCenario` para mapear a string da flag CLI para
   a struct concreta do teste.

RESULTADOS ESPERADOS:
- Extensibilidade total: novos cenários podem ser criados sem alterar o main.go.
- Isolamento estrito de dependências entre os fluxos de teste.
*/

type ContextoExecucao struct {
	Cliente        *ClienteTCPPersistente
	WorkerNome     string
	Origens        []string
	Destinos       []string
	Stats          *EstatisticasConsorcio
	ReservasLocais *[]int
}

type CenarioTeste interface {
	Nome() string
	ExecutarPasso(ctx *ContextoExecucao)
}

func ObterCenario(nome string) CenarioTeste {
	switch nome {
	case "busca":
		return &CenarioBusca{}
	case "contencao":
		return &CenarioContencao{}
	case "cancelamento":
		return &CenarioCancelamento{}
	case "motorista":
		return &CenarioResilienciaMotorista{}
	case "misto":
		fallthrough
	default:
		return &CenarioMisto{}
	}
}
