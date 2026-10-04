package protocolo

// Trecho representa um segmento contíguo dentro do anúncio de uma carona.
type Trecho struct {
	Origem         string   `json:"origem"`
	Destino        string   `json:"destino"`
	AssentosLivres int      `json:"assentos_livres"`
	Preco          float64  `json:"preco"`
	Passageiros    []string `json:"passageiros"`
}

// Carona representa o registro completo de uma carona publicada.
type Carona struct {
	ID             int      `json:"id"`
	Motorista      string   `json:"motorista"`
	Data           string   `json:"data"`
	Horario        string   `json:"horario"`
	AssentosTotais int      `json:"assentos_totais"`
	PrecoPorTrecho float64  `json:"preco_por_trecho"`
	Rota           []string `json:"rota"`
	Trechos        []Trecho `json:"trechos"`
}

// TrechoViagem identifica de forma única um segmento de viagem no consórcio (Empresa + CaronaID).
type TrechoViagem struct {
	Empresa   string  `json:"empresa"`
	CaronaID  int     `json:"carona_id"`
	Motorista string  `json:"motorista"`
	Origem    string  `json:"origem"`
	Destino   string  `json:"destino"`
	Horario   string  `json:"horario"`
	Preco     float64 `json:"preco"`
}

// Itinerario é uma composição de trechos conectando origem e destino do passageiro.
type Itinerario struct {
	Data       string         `json:"data"`
	PrecoTotal float64        `json:"preco_total"`
	Trechos    []TrechoViagem `json:"trechos"`
}

// Reserva representa a confirmação atômica gerada ao final do processo de reserva.
type Reserva struct {
	ID         int            `json:"id"`
	TxID       string         `json:"tx_id,omitempty"`
	Passageiro string         `json:"passageiro"`
	Data       string         `json:"data"`
	PrecoTotal float64        `json:"preco_total"`
	Trechos    []TrechoViagem `json:"trechos"`
}

// Notificacao define alertas emitidos para o passageiro.
type Notificacao struct {
	Passageiro string `json:"passageiro"`
	Mensagem   string `json:"mensagem"`
	Data       string `json:"data"`
}
