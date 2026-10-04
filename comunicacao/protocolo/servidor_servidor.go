package protocolo

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
