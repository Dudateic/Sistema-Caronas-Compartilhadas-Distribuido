package servicos

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/visual"
)

func responderJSON(conn net.Conn, v any) {
	b, _ := json.Marshal(v)
	b = append(b, '\n')
	_, _ = conn.Write(b)
}

// ProcessarPing responde ao handshake dos clientes: confirma que há um servidor VaiJunto atendendo.
func ProcessarPing(conn net.Conn) {
	responderJSON(conn, cliente_servidor.RespostaSimples{
		Tipo:     cliente_servidor.TipoPingRes,
		Sucesso:  true,
		Mensagem: "Servidor " + Config.Empresa + " pronto",
	})
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
	var req struct {
		Motorista string `json:"motorista"`
	}
	_ = json.Unmarshal([]byte(dados), &req)

	var caronas []modelos.Carona
	// Filtra as caronas pertencentes ao motorista solicitante (ou todas se não informado)
	for _, c := range InstanciaCaronas.caronas {
		if req.Motorista == "" || strings.EqualFold(c.Motorista, req.Motorista) {
			caronas = append(caronas, c)
		}
	}

	responderJSON(conn, cliente_servidor.RespostaConsultarCaronas{
		Tipo:     cliente_servidor.TipoConsultarCaronasRes,
		Sucesso:  true,
		Mensagem: "Caronas recuperadas",
		Caronas:  caronas,
	})
}

func ProcessarCancelarCarona(conn net.Conn, dados string) {
	var req struct {
		CaronaID  int    `json:"carona_id"`
		Motorista string `json:"motorista"`
	}
	_ = json.Unmarshal([]byte(dados), &req)

	err := InstanciaCaronas.CancelarCarona(req.CaronaID, req.Motorista)
	if err == nil {
		// 1. Limpa reservas e notifica passageiros locais
		if AoCancelarCarona != nil {
			AoCancelarCarona(req.CaronaID)
		}

		// 2. Notifica todos os servidores parceiros do consórcio
		go notificarConsorcioCaronaCancelada(Config.Empresa, req.CaronaID)
	}

	responderJSON(conn, cliente_servidor.RespostaSimples{
		Tipo:    cliente_servidor.TipoCancelarCaronaRes,
		Sucesso: err == nil,
		Mensagem: func() string {
			if err != nil {
				return err.Error()
			}
			return "Carona cancelada com sucesso"
		}(),
	})
}

func notificarConsorcioCaronaCancelada(empresaOrigem string, caronaID int) {
	clienteHTTP := &http.Client{Timeout: 3 * time.Second}
	payload, _ := json.Marshal(map[string]any{
		"empresa":   empresaOrigem,
		"carona_id": caronaID,
	})

	for _, urlBase := range Config.Pares {
		urlAlvo := fmt.Sprintf("%s/api/v1/caronas/cancelada", strings.TrimRight(urlBase, "/"))
		req, err := http.NewRequest(http.MethodPost, urlAlvo, bytes.NewReader(payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			resp, errDo := clienteHTTP.Do(req)
			if errDo == nil {
				_ = resp.Body.Close()
			}
		}
	}
}

func ProcessarBuscarItinerarios(conn net.Conn, dados string) {
	var req struct {
		Origem  string `json:"origem"`
		Destino string `json:"destino"`
		Data    string `json:"data"`
	}
	_ = json.Unmarshal([]byte(dados), &req)

	origemDesejada := strings.ToUpper(strings.TrimSpace(req.Origem))
	destinoDesejado := strings.ToUpper(strings.TrimSpace(req.Destino))
	dataDesejada := strings.TrimSpace(req.Data)

	todosTrechos := InstanciaCaronas.BuscarTrechos("", "")
	todosTrechos = append(todosTrechos, consultarTrechosDoConsorcio()...)

	var itinerarios []modelos.Itinerario
	visitados := make(map[string]bool)

	var dfs func(atual string, caminhoAtual []modelos.TrechoViagem, precoAcumulado float64)
	dfs = func(atual string, caminhoAtual []modelos.TrechoViagem, precoAcumulado float64) {
		if strings.EqualFold(atual, destinoDesejado) && len(caminhoAtual) > 0 {
			copiaCaminho := make([]modelos.TrechoViagem, len(caminhoAtual))
			copy(copiaCaminho, caminhoAtual)

			dataViagem := dataDesejada
			if dataViagem == "" && len(caminhoAtual) > 0 {
				dataViagem = copiaCaminho[0].Data
				if dataViagem == "" {
					dataViagem = time.Now().Format("2006-01-02")
				}
			}

			itinerarios = append(itinerarios, modelos.Itinerario{
				Data:       dataViagem,
				PrecoTotal: precoAcumulado,
				Trechos:    copiaCaminho,
			})
			return
		}

		visitados[strings.ToUpper(atual)] = true
		defer func() { visitados[strings.ToUpper(atual)] = false }()

		for _, t := range todosTrechos {
			tOrigem := strings.ToUpper(t.Origem)
			tDestino := strings.ToUpper(t.Destino)

			// Valida se o trecho coincide com a data solicitada pelo passageiro
			if dataDesejada != "" && t.Data != "" && !strings.EqualFold(t.Data, dataDesejada) {
				continue
			}

			if strings.EqualFold(tOrigem, atual) && !visitados[tDestino] {
				dfs(tDestino, append(caminhoAtual, t), precoAcumulado+t.Preco)
			}
		}
	}

	dfs(origemDesejada, nil, 0)

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
	if err := json.Unmarshal([]byte(dados), &req); err != nil {
		responderJSON(conn, cliente_servidor.RespostaSimples{
			Tipo:     cliente_servidor.TipoCancelarReservaRes,
			Sucesso:  false,
			Mensagem: "Payload invalido",
		})
		return
	}

	// 1. Cancela localmente e recupera os trechos da reserva
	reservaCancelada, err := InstanciaReservas.CancelarReserva(req.ReservaID, req.Passageiro)
	if err != nil {
		responderJSON(conn, cliente_servidor.RespostaSimples{
			Tipo:     cliente_servidor.TipoCancelarReservaRes,
			Sucesso:  false,
			Mensagem: err.Error(),
		})
		return
	}

	// 2. Avisa os servidores de cada trecho do itinerário
	var servidoresComFalha []string

	for _, trecho := range reservaCancelada.Trechos {
		// Se o trecho for desta própria empresa:
		if strings.EqualFold(trecho.Empresa, Config.Empresa) || trecho.Empresa == "" {
			if InstanciaCaronas != nil {
				_ = InstanciaCaronas.LiberarAssentoConfirmado(trecho.CaronaID, trecho.Origem, trecho.Destino, req.Passageiro)
			}
			continue
		}

		// Se o trecho for de um servidor parceiro:
		urlPar, existe := Config.Pares[trecho.Empresa]
		if !existe {
			continue
		}

		errRemoto := InstanciaClienteREST.EnviarCancelamentoTrecho(
			urlPar,
			trecho.CaronaID,
			trecho.Origem,
			trecho.Destino,
			req.Passageiro,
		)

		if errRemoto != nil {
			servidoresComFalha = append(servidoresComFalha, trecho.Empresa)
		}
	}

	msgRetorno := "Reserva cancelada com sucesso em todos os servidores"
	if len(servidoresComFalha) > 0 {
		msgRetorno = fmt.Sprintf(
			"Reserva cancelada localmente. O(s) servidor(es) %v estavam inacessiveis e serao atualizados automaticamente ao religarem.",
			servidoresComFalha,
		)
	}

	responderJSON(conn, cliente_servidor.RespostaSimples{
		Tipo:     cliente_servidor.TipoCancelarReservaRes,
		Sucesso:  true,
		Mensagem: msgRetorno,
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

// consultarTrechosDoConsorcio consulta todos os parceiros EM PARALELO (cada um com o seu
// timeout) e junta os resultados em ordem alfabética de empresa, para o resultado ser
// determinístico. Um parceiro fora do ar é registrado no log e simplesmente não contribui:
// as demais empresas continuam atendendo a busca.
func consultarTrechosDoConsorcio() []modelos.TrechoViagem {
	nomes := make([]string, 0, len(Config.Pares))
	for nome := range Config.Pares {
		nomes = append(nomes, nome)
	}
	sort.Strings(nomes)

	resultados := make([][]modelos.TrechoViagem, len(nomes))
	var wg sync.WaitGroup
	for i, nome := range nomes {
		wg.Add(1)
		go func(i int, nome, url string) {
			defer wg.Done()
			remotos, err := InstanciaClienteREST.ConsultarTrechos(url, "", "")
			if err != nil {
				visual.MensagemErro(fmt.Sprintf("[Busca] Falha ao consultar a empresa %s em %s: %v", nome, url, err))
				return
			}
			resultados[i] = remotos
		}(i, nome, Config.Pares[nome])
	}
	wg.Wait()

	var todos []modelos.TrechoViagem
	for _, r := range resultados {
		todos = append(todos, r...)
	}
	return todos
}
