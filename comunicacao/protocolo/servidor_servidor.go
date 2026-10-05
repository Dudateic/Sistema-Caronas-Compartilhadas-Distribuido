package protocolo

const (
	RotaPrepare  = "/api/v1/transacoes/prepare"
	RotaCommit   = "/api/v1/transacoes/%s/commit"   // %s = tx_id
	RotaRollback = "/api/v1/transacoes/%s/rollback" // %s = tx_id
)

type ErroHTTP struct {
	Status   int    `json:"status"`
	Mensagem string `json:"mensagem"`
}

type PedidoBloqueio struct {
	TxID       string `json:"tx_id"`
	CaronaID   int    `json:"carona_id"`
	Origem     string `json:"origem"`
	Destino    string `json:"destino"`
	Passageiro string `json:"passageiro"`
	Data       string `json:"data"`
	Timestamp  int64  `json:"timestamp"`
}

type PedidoCommit struct {
	TxID string `json:"tx_id"`
}

type PedidoRollback struct {
	TxID   string `json:"tx_id"`
	Motivo string `json:"motivo,omitempty"`
}

type RespostaPrepare struct {
	TxID   string  `json:"tx_id"`
	Voto   bool    `json:"voto"`
	Motivo string  `json:"motivo,omitempty"`
	Preco  float64 `json:"preco,omitempty"`

	Bloqueada       bool   `json:"bloqueada,omitempty"`
	HolderTimestamp int64  `json:"holder_timestamp,omitempty"`
	HolderTxID      string `json:"holder_tx_id,omitempty"`

	Relogio int64 `json:"relogio"`
}

type RespostaTransacao struct {
	TxID     string `json:"tx_id"`
	Estado   string `json:"estado"`
	Mensagem string `json:"mensagem,omitempty"`
}
