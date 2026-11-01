package modelos

type EstadoTransacao string

const (
	TxIniciada   EstadoTransacao = "INICIADA"
	TxPreparada  EstadoTransacao = "PREPARADA"
	TxConfirmada EstadoTransacao = "CONFIRMADA"
	TxAbortada   EstadoTransacao = "ABORTADA"
)

type Transacao struct {
	ID        string          `json:"id"`
	RecursoID string          `json:"recurso_id"`
	Estado    EstadoTransacao `json:"estado"`
	Timestamp int64           `json:"Lamport"`
}

type PrepareRequest struct {
	TxID       string `json:"tx_id"`
	CaronaID   int    `json:"carona_id"`
	Origem     string `json:"origem"`
	Destino    string `json:"destino"`
	Passageiro string `json:"passageiro"`
	Timestamp  int64  `json:"timestamp"`
	// Coordenador é o nome da empresa que coordena a transação. O participante
	// usa esse nome para perguntar a decisão global caso fique sem resposta.
	Coordenador string `json:"coordenador,omitempty"`
	// Participantes lista todas as empresas da transação. Se o coordenador cair, os
	// participantes perguntam uns aos outros o destino da transação (terminação cooperativa).
	Participantes []string `json:"participantes,omitempty"`
}

type PrepareResponse struct {
	TxID      string `json:"tx_id"`
	VotoSim   bool   `json:"voto_sim"`
	Motivo    string `json:"motivo,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// Decisões globais devolvidas por GET /api/v1/transacoes/decisao.
const (
	DecisaoCommit      = "COMMIT"
	DecisaoAbort       = "ABORT"
	DecisaoEmAndamento = "EM_ANDAMENTO"
)

// Estados de uma transação em um participante, devolvidos por GET /api/v1/transacoes/estado.
const (
	EstadoPreparada    = "PREPARADA"
	EstadoConfirmada   = "CONFIRMADA"
	EstadoAbortada     = "ABORTADA"
	EstadoDesconhecida = "DESCONHECIDA"
)

type EstadoResponse struct {
	TxID   string `json:"tx_id"`
	Estado string `json:"estado"`
}

type DecisaoResponse struct {
	TxID    string `json:"tx_id"`
	Decisao string `json:"decisao"`
}

type DecisaoRequest struct {
	TxID      string `json:"tx_id"`
	Confirmar bool   `json:"confirmar"`
}

type TrechoOcupadoInfo struct {
	CaronaID   int    `json:"carona_id"`
	Origem     string `json:"origem"`
	Destino    string `json:"destino"`
	Passageiro string `json:"passageiro"`
}

type SyncStatusRequest struct {
	EmpresaSolitante string              `json:"empresa_solicitante"`
	TrechosAtivos    []TrechoOcupadoInfo `json:"trechos_ativos"`
}

type SyncStatusResponse struct {
	TrechosParaLiberar []TrechoOcupadoInfo `json:"trechos_para_liberar"`
}
