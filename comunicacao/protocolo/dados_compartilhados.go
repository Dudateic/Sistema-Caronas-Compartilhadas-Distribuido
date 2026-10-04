package protocolo

const (
	TipoLoginReq    = "login"
	TipoCadastroReq = "cadastro"

	TipoPublicarCaronaReq   = "publicar_carona"
	TipoConsultarCaronasReq = "consultar_caronas_motorista"
	TipoCancelarCaronaReq   = "cancelar_carona"

	TipoBuscarItinerariosReq     = "buscar_itinerarios"
	TipoReservarItinerarioReq    = "reservar_itinerario"
	TipoConsultarReservasReq     = "consultar_reservas_passageiro"
	TipoCancelarReservaReq       = "cancelar_reserva_passageiro"
	TipoConsultarNotificacoesReq = "consultar_notificacoes"
)

type MensagemBase struct {
	Tipo string `json:"tipo"`
}

type CredencialLogin struct {
	Tipo    string `json:"tipo"`
	Usuario string `json:"usuario"`
	Senha   string `json:"senha"`
}

type RegistroUsuario struct {
	Tipo        string `json:"tipo"`
	IDUsuario   string `json:"id_usuario"`
	Usuario     string `json:"usuario"`
	Senha       string `json:"senha"`
	TipoUsuario string `json:"tipo_usuario"`
}

type PublicacaoCarona struct {
	Tipo           string   `json:"tipo"`
	Motorista      string   `json:"motorista"`
	Rota           []string `json:"rota"`
	Data           string   `json:"data"`
	Horario        string   `json:"horario"`
	AssentosTotais int      `json:"assentos_totais"`
	PrecoPorTrecho float64  `json:"preco_por_trecho"`
}

type ConsultaCaronasMotorista struct {
	Tipo      string `json:"tipo"`
	Motorista string `json:"motorista"`
}

type CancelamentoCarona struct {
	Tipo      string `json:"tipo"`
	CaronaID  int    `json:"carona_id"`
	Motorista string `json:"motorista"`
}

type ConsultaItinerario struct {
	Tipo    string `json:"tipo"`
	Origem  string `json:"origem"`
	Destino string `json:"destino"`
	Data    string `json:"data"`
}

type SolicitacaoReserva struct {
	Tipo       string         `json:"tipo"`
	Passageiro string         `json:"passageiro"`
	Data       string         `json:"data"`
	Trechos    []TrechoViagem `json:"trechos"`
}

type ConsultaReservasPassageiro struct {
	Tipo       string `json:"tipo"`
	Passageiro string `json:"passageiro"`
}

type CancelamentoReserva struct {
	Tipo       string `json:"tipo"`
	ReservaID  int    `json:"reserva_id"`
	Passageiro string `json:"passageiro"`
}

type ConsultaNotificacoes struct {
	Tipo       string `json:"tipo"`
	Passageiro string `json:"passageiro"`
}
