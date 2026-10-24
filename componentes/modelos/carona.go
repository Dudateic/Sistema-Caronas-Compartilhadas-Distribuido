package modelos

type Trecho struct {
	Origem         string   `json:"origem"`
	Destino        string   `json:"destino"`
	AssentosLivres int      `json:"assentos_livres"`
	Preco          float64  `json:"preco"`
	Passageiros    []string `json:"passageiros"`
}

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

type TrechoViagem struct {
	Empresa   string  `json:"empresa"`
	CaronaID  int     `json:"carona_id"`
	Motorista string  `json:"motorista"`
	Origem    string  `json:"origem"`
	Destino   string  `json:"destino"`
	Data      string  `json:"data"`
	Horario   string  `json:"horario"`
	Preco     float64 `json:"preco"`
}

type Itinerario struct {
	Data       string         `json:"data"`
	PrecoTotal float64        `json:"preco_total"`
	Trechos    []TrechoViagem `json:"trechos"`
}

type Reserva struct {
	ID         int            `json:"id"`
	TxID       string         `json:"tx_id,omitempty"`
	Passageiro string         `json:"passageiro"`
	Data       string         `json:"data"`
	PrecoTotal float64        `json:"preco_total"`
	Trechos    []TrechoViagem `json:"trechos"`
}

type Notificacao struct {
	Passageiro string `json:"passageiro"`
	Mensagem   string `json:"mensagem"`
	Data       string `json:"data"`
}
