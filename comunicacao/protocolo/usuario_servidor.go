package protocolo

const (
	TipoLoginReq                 = "login_requisicao"
	TipoCadastroReq              = "cadastro_requisicao"
	TipoPublicarCaronaReq        = "publicar_carona_requisicao"
	TipoConsultarCaronasReq      = "consultar_caronas_requisicao"
	TipoCancelarCaronaReq        = "cancelar_carona_requisicao"
	TipoBuscarItinerariosReq     = "buscar_itinerarios_requisicao"
	TipoReservarItinerarioReq    = "reservar_requisicao"
	TipoConsultarReservasReq     = "consultar_reservas_requisicao"
	TipoCancelarReservaReq       = "cancelar_reserva_requisicao"
	TipoConsultarNotificacoesReq = "consultar_notificacoes_requisicao"

	TipoLoginRes                 = "login_resposta"
	TipoCadastroRes              = "cadastro_resposta"
	TipoPublicarCaronaRes        = "publicar_carona_resposta"
	TipoConsultarCaronasRes      = "consultar_caronas_resposta"
	TipoCancelarCaronaRes        = "cancelar_carona_resposta"
	TipoBuscarItinerariosRes     = "buscar_itinerarios_resposta"
	TipoReservarItinerarioRes    = "reservar_resposta"
	TipoConsultarReservasRes     = "consultar_reservas_resposta"
	TipoCancelarReservaRes       = "cancelar_reserva_resposta"
	TipoConsultarNotificacoesRes = "consultar_notificacoes_resposta"
)

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

// Resposta para ações sem retorno de dados (ex: cadastro, cancelamentos).
type RespostaSimples struct {
	Tipo     string `json:"tipo"`
	Sucesso  bool   `json:"sucesso"`
	Mensagem string `json:"mensagem"`
}

// informa o resultado da autenticação e as credenciais da sessão.
type RespostaLogin struct {
	Tipo        string `json:"tipo"`
	Sucesso     bool   `json:"sucesso"`
	Mensagem    string `json:"mensagem"`
	IDUsuario   string `json:"id_usuario,omitempty"`
	TipoUsuario string `json:"tipo_usuario,omitempty"`
}

// retorna o identificador único gerado para a nova carona.
type RespostaPublicarCarona struct {
	Tipo     string `json:"tipo"`
	Sucesso  bool   `json:"sucesso"`
	Mensagem string `json:"mensagem"`
	CaronaID int    `json:"carona_id,omitempty"`
}

// devolve a lista de caronas pertencentes ao motorista.
type RespostaConsultarCaronas struct {
	Tipo     string   `json:"tipo"`
	Sucesso  bool     `json:"sucesso"`
	Mensagem string   `json:"mensagem"`
	Caronas  []Carona `json:"caronas,omitempty"`
}

// retorna as combinações de trechos encontradas pelo algoritmo de busca.
type RespostaBuscarItinerarios struct {
	Tipo        string       `json:"tipo"`
	Sucesso     bool         `json:"sucesso"`
	Mensagem    string       `json:"mensagem"`
	Itinerarios []Itinerario `json:"itinerarios,omitempty"`
}

// devolve o identificador gerado após o commit da reserva atômica.
type RespostaReservar struct {
	Tipo      string `json:"tipo"`
	Sucesso   bool   `json:"sucesso"`
	Mensagem  string `json:"mensagem"`
	ReservaID int    `json:"reserva_id,omitempty"`
}

// lista as reservas ativas do passageiro solicitante.
type RespostaConsultarReservas struct {
	Tipo     string    `json:"tipo"`
	Sucesso  bool      `json:"sucesso"`
	Mensagem string    `json:"mensagem"`
	Reservas []Reserva `json:"reservas,omitempty"`
}

// entrega os avisos pendentes do passageiro.
type RespostaConsultarNotificacoes struct {
	Tipo         string        `json:"tipo"`
	Sucesso      bool          `json:"sucesso"`
	Notificacoes []Notificacao `json:"notificacoes"`
}
