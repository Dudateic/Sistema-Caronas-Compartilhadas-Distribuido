package cliente_servidor

import (
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

const (
	TipoPingReq = "ping_requisicao"
	TipoPingRes = "ping_resposta"

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

type RespostaSimples struct {
	Tipo     string `json:"tipo"`
	Sucesso  bool   `json:"sucesso"`
	Mensagem string `json:"mensagem"`
}

type RespostaLogin struct {
	Tipo        string `json:"tipo"`
	Sucesso     bool   `json:"sucesso"`
	Mensagem    string `json:"mensagem"`
	IDUsuario   string `json:"id_usuario,omitempty"`
	TipoUsuario string `json:"tipo_usuario,omitempty"`
}

type RespostaPublicarCarona struct {
	Tipo     string `json:"tipo"`
	Sucesso  bool   `json:"sucesso"`
	Mensagem string `json:"mensagem"`
	CaronaID int    `json:"carona_id,omitempty"`
}

type RespostaConsultarCaronas struct {
	Tipo     string           `json:"tipo"`
	Sucesso  bool             `json:"sucesso"`
	Mensagem string           `json:"mensagem"`
	Caronas  []modelos.Carona `json:"caronas,omitempty"`
}

type RespostaBuscarItinerarios struct {
	Tipo        string               `json:"tipo"`
	Sucesso     bool                 `json:"sucesso"`
	Mensagem    string               `json:"mensagem"`
	Itinerarios []modelos.Itinerario `json:"itinerarios"`
}

type RespostaReservar struct {
	Tipo      string `json:"tipo"`
	Sucesso   bool   `json:"sucesso"`
	Mensagem  string `json:"mensagem"`
	ReservaID int    `json:"reserva_id,omitempty"`
}

type RespostaConsultarReservas struct {
	Tipo     string            `json:"tipo"`
	Sucesso  bool              `json:"sucesso"`
	Mensagem string            `json:"mensagem"`
	Reservas []modelos.Reserva `json:"reservas,omitempty"`
}

type RespostaConsultarNotificacoes struct {
	Tipo         string                `json:"tipo"`
	Sucesso      bool                  `json:"sucesso"`
	Mensagem     string                `json:"mensagem"`
	Notificacoes []modelos.Notificacao `json:"notificacoes"`
}
