package simulador

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
