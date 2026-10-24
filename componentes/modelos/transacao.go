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
}

type PrepareResponse struct {
	TxID    string `json:"tx_id"`
	VotoSim bool   `json:"voto_sim"`
	Motivo  string `json:"motivo,omitempty"`
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
