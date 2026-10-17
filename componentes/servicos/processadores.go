package servicos

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

func responderJSON(conn net.Conn, v any) {
	b, _ := json.Marshal(v)
	b = append(b, '\n')
	_, _ = conn.Write(b)
}

func ProcessarCadastro(conn net.Conn, dados string) {
	var req struct {
		Nome        string              `json:"nome"`
		Login       string              `json:"login"`
		Senha       string              `json:"senha"`
		TipoUsuario modelos.TipoUsuario `json:"tipo_usuario"`
	}
	_ = json.Unmarshal([]byte(dados), &req)
	err := InstanciaUsuarios.Cadastrar(req.Nome, req.Login, req.Senha, req.TipoUsuario)
	responderJSON(conn, cliente_servidor.RespostaSimples{
		Tipo:    cliente_servidor.TipoCadastroRes,
		Sucesso: err == nil,
		Mensagem: func() string {
			if err != nil {
				return err.Error()
			}
			return "Cadastro realizado com sucesso"
		}(),
	})
}

func ProcessarLogin(conn net.Conn, dados string) {
	var req struct {
		Login string `json:"login"`
		Senha string `json:"senha"`
	}
	_ = json.Unmarshal([]byte(dados), &req)
	sessao, err := InstanciaUsuarios.Autenticar(req.Login, req.Senha)
	if err != nil {
		responderJSON(conn, cliente_servidor.RespostaLogin{
			Tipo:     cliente_servidor.TipoLoginRes,
			Sucesso:  false,
			Mensagem: err.Error(),
		})
		return
	}
	responderJSON(conn, cliente_servidor.RespostaLogin{
		Tipo:        cliente_servidor.TipoLoginRes,
		Sucesso:     true,
		Mensagem:    "Login realizado com sucesso",
		IDUsuario:   sessao.IDUsuario,
		TipoUsuario: string(sessao.Tipo),
	})
}

func ProcessarPublicarCarona(conn net.Conn, dados string) {
	var req struct {
		Motorista      string   `json:"motorista"`
		Data           string   `json:"data"`
		Horario        string   `json:"horario"`
		Rota           []string `json:"rota"`
		AssentosTotais int      `json:"assentos_totais"`
		PrecoPorTrecho float64  `json:"preco_por_trecho"`
	}
	_ = json.Unmarshal([]byte(dados), &req)
	id, err := InstanciaCaronas.PublicarCarona(req.Motorista, req.Data, req.Horario, req.Rota, req.AssentosTotais, req.PrecoPorTrecho)
	responderJSON(conn, cliente_servidor.RespostaPublicarCarona{
		Tipo:    cliente_servidor.TipoPublicarCaronaRes,
		Sucesso: err == nil,
		Mensagem: func() string {
			if err != nil {
				return err.Error()
			}
			return "Carona publicada com sucesso"
		}(),
		CaronaID: id,
	})
}

func ProcessarConsultarCaronas(conn net.Conn, dados string) {
	trechos := InstanciaCaronas.BuscarTrechos("", "")
	var caronas []modelos.Carona
	// Devolve as caronas ativas da empresa
	for _, c := range InstanciaCaronas.caronas {
		caronas = append(caronas, c)
	}
	_ = trechos
	responderJSON(conn, cliente_servidor.RespostaConsultarCaronas{
		Tipo:     cliente_servidor.TipoConsultarCaronasRes,
		Sucesso:  true,
		Mensagem: "Caronas recuperadas",
		Caronas:  caronas,
	})
}

func ProcessarCancelarCarona(conn net.Conn, dados string) {
	var req struct {
		CaronaID int `json:"carona_id"`
	}
	_ = json.Unmarshal([]byte(dados), &req)
	// Cancelamento de carona
	if AoCancelarCarona != nil {
		AoCancelarCarona(req.CaronaID)
	}
	responderJSON(conn, cliente_servidor.RespostaSimples{
		Tipo:     cliente_servidor.TipoCancelarCaronaRes,
		Sucesso:  true,
		Mensagem: "Carona cancelada com sucesso",
	})
}

func ProcessarBuscarItinerarios(conn net.Conn, dados string) {
	var req struct {
		Origem  string `json:"origem"`
		Destino string `json:"destino"`
	}
	_ = json.Unmarshal([]byte(dados), &req)

	trechos := InstanciaCaronas.BuscarTrechos(req.Origem, req.Destino)

	// Consulta nós parceiros via REST
	for _, urlPar := range Config.Pares {
		remotos, err := InstanciaClienteREST.ConsultarTrechos(urlPar, req.Origem, req.Destino)
		if err == nil {
			trechos = append(trechos, remotos...)
		}
	}

	var itinerarios []modelos.Itinerario
	for _, t := range trechos {
		itinerarios = append(itinerarios, modelos.Itinerario{
			Data:       "2026-10-27",
			PrecoTotal: t.Preco,
			Trechos:    []modelos.TrechoViagem{t},
		})
	}

	responderJSON(conn, cliente_servidor.RespostaBuscarItinerarios{
		Tipo:        cliente_servidor.TipoBuscarItinerariosRes,
		Sucesso:     true,
		Mensagem:    "Busca concluida",
		Itinerarios: itinerarios,
	})
}

func ProcessarReservarItinerario(conn net.Conn, dados string) {
	var req struct {
		Passageiro string             `json:"passageiro"`
		Itinerario modelos.Itinerario `json:"itinerario"`
	}
	_ = json.Unmarshal([]byte(dados), &req)

	txID := fmt.Sprintf("tx_%s_%d", Config.Empresa, time.Now().UnixNano())
	sucesso, motivo, _ := InstanciaCoordenador.ExecutarReservaAtomica(txID, req.Passageiro, req.Itinerario, time.Now().Unix())

	if sucesso {
		reserva := InstanciaReservas.CriarReservaConfirmada(txID, req.Passageiro, req.Itinerario)
		responderJSON(conn, cliente_servidor.RespostaReservar{
			Tipo:      cliente_servidor.TipoReservarItinerarioRes,
			Sucesso:   true,
			Mensagem:  "Reserva confirmada no consorcio",
			ReservaID: reserva.ID,
		})
	} else {
		responderJSON(conn, cliente_servidor.RespostaReservar{
			Tipo:     cliente_servidor.TipoReservarItinerarioRes,
			Sucesso:  false,
			Mensagem: motivo,
		})
	}
}

func ProcessarConsultarReservas(conn net.Conn, dados string) {
	var req struct {
		Passageiro string `json:"passageiro"`
	}
	_ = json.Unmarshal([]byte(dados), &req)
	reservas := InstanciaReservas.ConsultarReservasPassageiro(req.Passageiro)
	responderJSON(conn, cliente_servidor.RespostaConsultarReservas{
		Tipo:     cliente_servidor.TipoConsultarReservasRes,
		Sucesso:  true,
		Mensagem: "Reservas localizadas",
		Reservas: reservas,
	})
}

func ProcessarCancelarReserva(conn net.Conn, dados string) {
	var req struct {
		ReservaID  int    `json:"reserva_id"`
		Passageiro string `json:"passageiro"`
	}
	_ = json.Unmarshal([]byte(dados), &req)
	_, err := InstanciaReservas.CancelarReserva(req.ReservaID, req.Passageiro)
	responderJSON(conn, cliente_servidor.RespostaSimples{
		Tipo:    cliente_servidor.TipoCancelarReservaRes,
		Sucesso: err == nil,
		Mensagem: func() string {
			if err != nil {
				return err.Error()
			}
			return "Reserva cancelada"
		}(),
	})
}

func ProcessarConsultarNotificacoes(conn net.Conn, dados string) {
	var req struct {
		Passageiro string `json:"passageiro"`
	}
	_ = json.Unmarshal([]byte(dados), &req)
	notificacoes := InstanciaReservas.ConsultarNotificacoes(req.Passageiro)
	responderJSON(conn, cliente_servidor.RespostaConsultarNotificacoes{
		Tipo:         cliente_servidor.TipoConsultarNotificacoesRes,
		Sucesso:      true,
		Mensagem:     "Notificacoes carregadas",
		Notificacoes: notificacoes,
	})
}
