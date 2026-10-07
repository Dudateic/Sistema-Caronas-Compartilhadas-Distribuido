package service

import (
	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

var (
	ReservasRegistradas []protocolo.Reserva
	ProximoReservaID    = 1
	MutexReservas       sync.Mutex
)

const ArquivoReservas = "reservas.json"

var NotificacoesRegistradas []protocolo.Notificacao

const ArquivoNotificacoes = "notificacoes.json"

// InicializarReservas carrega o estado guardado no disco para a memória
func InicializarReservas() error {
	MutexReservas.Lock()
	defer MutexReservas.Unlock()

	if err := CarregarJSON(ArquivoReservas, &ReservasRegistradas); err != nil {
		return err
	}

	for _, r := range ReservasRegistradas {
		if r.ID >= ProximoReservaID {
			ProximoReservaID = r.ID + 1
		}
	}
	return nil
}

func ProcessarBuscarItinerarios(conn net.Conn, dadosBrutos string) {
	var req protocolo.ConsultaItinerario
	if err := json.Unmarshal([]byte(dadosBrutos), &req); err != nil {
		responderBusca(conn, false, "JSON malformado", nil)
		return
	}

	grafoAdjacenciaTrechos := make(map[string][]protocolo.TrechoViagem)

	MutexCaronas.RLock()
	for _, c := range CaronasRegistradas {
		if c.Data != req.Data {
			continue
		}
		for _, t := range c.Trechos {
			// Vagas presas por reservas em andamento (2PC) nao contam como livres.
			if t.AssentosLivres-Bloqueados(c.ID, t.Origem, t.Destino) > 0 {
				novoTrecho := protocolo.TrechoViagem{
					Empresa:   Config.Empresa,
					CaronaID:  c.ID,
					Motorista: c.Motorista,
					Origem:    t.Origem,
					Destino:   t.Destino,
					Horario:   c.Horario,
					Preco:     t.Preco,
				}
				grafoAdjacenciaTrechos[t.Origem] = append(grafoAdjacenciaTrechos[t.Origem], novoTrecho)
			}
		}
	}
	MutexCaronas.RUnlock()

	var itinerariosEncontrados []protocolo.Itinerario
	visitados := make(map[string]bool)

	buscarDFS(
		req.Origem,
		req.Destino,
		req.Data,
		grafoAdjacenciaTrechos,
		visitados,
		[]protocolo.TrechoViagem{},
		0.0,
		&itinerariosEncontrados,
	)

	if len(itinerariosEncontrados) == 0 {
		responderBusca(conn, true, "Nenhum itinerario disponivel para essa data", []protocolo.Itinerario{})
		return
	}

	msg := fmt.Sprintf("%d itinerario(s) encontrado(s)", len(itinerariosEncontrados))
	responderBusca(conn, true, msg, itinerariosEncontrados)
}

func buscarDFS(
	atual string,
	destino string,
	data string,
	grafo map[string][]protocolo.TrechoViagem,
	visitados map[string]bool,
	caminho []protocolo.TrechoViagem,
	custo float64,
	resultados *[]protocolo.Itinerario,
) {
	if atual == destino {
		trechosCopia := make([]protocolo.TrechoViagem, len(caminho))
		copy(trechosCopia, caminho)

		*resultados = append(*resultados, protocolo.Itinerario{
			Data:       data,
			PrecoTotal: custo,
			Trechos:    trechosCopia,
		})
		return
	}

	visitados[atual] = true

	for _, tr := range grafo[atual] {
		if !visitados[tr.Destino] {
			if len(caminho) > 0 {
				ultimoTrecho := caminho[len(caminho)-1]

				if ultimoTrecho.CaronaID != tr.CaronaID {
					minutosUltimo := converterParaMinutos(ultimoTrecho.Horario)
					minutosAtual := converterParaMinutos(tr.Horario)

					if minutosAtual < minutosUltimo {
						continue
					}
				}
			}

			caminho = append(caminho, tr)
			buscarDFS(tr.Destino, destino, data, grafo, visitados, caminho, custo+tr.Preco, resultados)
			caminho = caminho[:len(caminho)-1]
		}
	}

	visitados[atual] = false
}

func responderBusca(conn net.Conn, sucesso bool, mensagem string, itinerarios []protocolo.Itinerario) {
	resp := protocolo.RespostaBuscarItinerarios{
		Tipo:        protocolo.TipoBuscarItinerariosRes,
		Sucesso:     sucesso,
		Mensagem:    mensagem,
		Itinerarios: itinerarios,
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

// ProcessarReservarItinerario recebe o pedido do passageiro e atua como COORDENADOR
// do Two-Phase Commit (ver coordenador.go): tudo ou nada, mesmo entre empresas.
func ProcessarReservarItinerario(conn net.Conn, dadosBrutos string) {
	var req protocolo.SolicitacaoReserva
	if err := json.Unmarshal([]byte(dadosBrutos), &req); err != nil {
		responderReserva(conn, false, "JSON malformado", 0)
		return
	}

	if len(req.Trechos) == 0 || req.Passageiro == "" {
		responderReserva(conn, false, "Parametros de reserva invalidos", 0)
		return
	}

	id, msg, ok := CoordenarReserva(req)
	responderReserva(conn, ok, msg, id)
}

func responderReserva(conn net.Conn, sucesso bool, mensagem string, id int) {
	resp := protocolo.RespostaReservar{
		Tipo:      protocolo.TipoReservarItinerarioRes,
		Sucesso:   sucesso,
		Mensagem:  mensagem,
		ReservaID: id,
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func ProcessarConsultarReservas(conn net.Conn, dadosBrutos string) {
	var req protocolo.ConsultaReservasPassageiro
	if err := json.Unmarshal([]byte(dadosBrutos), &req); err != nil {
		return
	}

	MutexReservas.Lock()
	var minhas []protocolo.Reserva
	for _, res := range ReservasRegistradas {
		if res.Passageiro == req.Passageiro {
			minhas = append(minhas, res)
		}
	}
	MutexReservas.Unlock()

	resp := protocolo.RespostaConsultarReservas{
		Tipo:     protocolo.TipoConsultarReservasRes,
		Sucesso:  true,
		Mensagem: "OK",
		Reservas: minhas,
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func ProcessarCancelarReserva(conn net.Conn, dadosBrutos string) {
	var req protocolo.CancelamentoReserva
	if err := json.Unmarshal([]byte(dadosBrutos), &req); err != nil {
		return
	}

	MutexCaronas.Lock()
	defer MutexCaronas.Unlock()

	MutexReservas.Lock()
	defer MutexReservas.Unlock()

	idxReserva := -1
	var reservaAlvo protocolo.Reserva
	for i, res := range ReservasRegistradas {
		if res.ID == req.ReservaID && res.Passageiro == req.Passageiro {
			idxReserva = i
			reservaAlvo = res
			break
		}
	}

	if idxReserva == -1 {
		resp := protocolo.RespostaSimples{
			Tipo:     protocolo.TipoCancelarReservaRes,
			Sucesso:  false,
			Mensagem: "Reserva nao encontrada ou permissao negada",
		}
		_ = json.NewEncoder(conn).Encode(resp)
		return
	}

	ReservasRegistradas = append(ReservasRegistradas[:idxReserva], ReservasRegistradas[idxReserva+1:]...)

	// Libera apenas os trechos desta empresa. (Trechos de outras empresas: ver limitacoes.)
	for _, trReq := range reservaAlvo.Trechos {
		if !ehLocal(trReq.Empresa) {
			continue
		}
		for i := range CaronasRegistradas {
			if CaronasRegistradas[i].ID == trReq.CaronaID {
				for j := range CaronasRegistradas[i].Trechos {
					tr := &CaronasRegistradas[i].Trechos[j]
					if tr.Origem == trReq.Origem && tr.Destino == trReq.Destino {
						tr.AssentosLivres++
						for pIdx, pNome := range tr.Passageiros {
							if pNome == req.Passageiro {
								tr.Passageiros = append(tr.Passageiros[:pIdx], tr.Passageiros[pIdx+1:]...)
								break
							}
						}
					}
				}
			}
		}
	}

	if err := SalvarJSON(ArquivoReservas, ReservasRegistradas); err != nil {
		fmt.Printf("[ERRO] Falha ao persistir reservas apos cancelamento: %v\n", err)
	}
	if err := SalvarJSON(ArquivoCaronas, CaronasRegistradas); err != nil {
		fmt.Printf("[ERRO] Falha ao persistir caronas apos restauracao de vagas: %v\n", err)
	}

	fmt.Printf("[RESERVA] Reserva #%d cancelada para '%s'\n", req.ReservaID, req.Passageiro)

	resp := protocolo.RespostaSimples{
		Tipo:     protocolo.TipoCancelarReservaRes,
		Sucesso:  true,
		Mensagem: "Reserva cancelada e assentos liberados com sucesso",
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func LimparReservasDaCarona(caronaID int, motorista string) {
	MutexReservas.Lock()
	defer MutexReservas.Unlock()

	_ = CarregarJSON(ArquivoNotificacoes, &NotificacoesRegistradas)

	var reservasRestantes []protocolo.Reserva
	for _, res := range ReservasRegistradas {
		afetada := false
		for _, tr := range res.Trechos {
			// O ID da carona so identifica a carona dentro da empresa dona dela.
			if ehLocal(tr.Empresa) && tr.CaronaID == caronaID {
				afetada = true
				break
			}
		}

		if afetada {
			msg := fmt.Sprintf("ATENCAO: Sua reserva #%d foi cancelada pelo sistema porque o motorista '%s' cancelou a carona.", res.ID, motorista)

			novaNot := protocolo.Notificacao{
				Passageiro: res.Passageiro,
				Mensagem:   msg,
				Data:       time.Now().Format("2006-01-02 15:04:05"),
			}
			NotificacoesRegistradas = append(NotificacoesRegistradas, novaNot)
			fmt.Printf("[NOTIFICACAO] Aviso gerado para o passageiro '%s'\n", res.Passageiro)
		} else {
			reservasRestantes = append(reservasRestantes, res)
		}
	}

	ReservasRegistradas = reservasRestantes
	_ = SalvarJSON(ArquivoReservas, ReservasRegistradas)
	_ = SalvarJSON(ArquivoNotificacoes, NotificacoesRegistradas)
}

func ProcessarConsultarNotificacoes(conn net.Conn, dadosBrutos string) {
	var req protocolo.ConsultaNotificacoes
	if err := json.Unmarshal([]byte(dadosBrutos), &req); err != nil {
		return
	}

	MutexReservas.Lock()
	defer MutexReservas.Unlock()
	_ = CarregarJSON(ArquivoNotificacoes, &NotificacoesRegistradas)

	var minhas []protocolo.Notificacao
	var restantes []protocolo.Notificacao

	for _, n := range NotificacoesRegistradas {
		if n.Passageiro == req.Passageiro {
			minhas = append(minhas, n)
		} else {
			restantes = append(restantes, n)
		}
	}

	if len(minhas) > 0 {
		NotificacoesRegistradas = restantes
		_ = SalvarJSON(ArquivoNotificacoes, NotificacoesRegistradas)
	}

	resp := protocolo.RespostaConsultarNotificacoes{
		Tipo:         protocolo.TipoConsultarNotificacoesRes,
		Sucesso:      true,
		Notificacoes: minhas,
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func converterParaMinutos(horario string) int {
	var h, m int
	_, _ = fmt.Sscanf(horario, "%d:%d", &h, &m)
	return h*60 + m
}
