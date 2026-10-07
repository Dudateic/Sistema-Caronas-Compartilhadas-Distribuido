package service

import (
	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
	"encoding/json"
	"fmt"
	"net"
	"sync"
)

var (
	CaronasRegistradas []protocolo.Carona
	ProximoCaronaID    = 1
	MutexCaronas       sync.RWMutex
)

const ArquivoCaronas = "caronas.json"

var AoCancelarCarona func(caronaID int, motorista string)

func InicializarCaronas() error {
	MutexCaronas.Lock()
	defer MutexCaronas.Unlock()

	if err := CarregarJSON(ArquivoCaronas, &CaronasRegistradas); err != nil {
		return err
	}

	for _, c := range CaronasRegistradas {
		if c.ID >= ProximoCaronaID {
			ProximoCaronaID = c.ID + 1
		}
	}
	return nil
}

func ProcessarPublicarCarona(conn net.Conn, dadosBrutos string) {
	var req protocolo.PublicacaoCarona
	if err := json.Unmarshal([]byte(dadosBrutos), &req); err != nil {
		responderPublicacao(conn, false, "JSON malformado", 0)
		return
	}

	if len(req.Rota) < 2 {
		responderPublicacao(conn, false, "A rota precisa de pelo menos 2 cidades", 0)
		return
	}

	if req.AssentosTotais <= 0 {
		responderPublicacao(conn, false, "O total de assentos deve ser maior que zero", 0)
		return
	}

	if req.PrecoPorTrecho <= 0 {
		responderPublicacao(conn, false, "O preco por trecho deve ser positivo", 0)
		return
	}

	MutexCaronas.Lock()
	defer MutexCaronas.Unlock()

	var trechos []protocolo.Trecho
	for i := 0; i < len(req.Rota)-1; i++ {
		trechos = append(trechos, protocolo.Trecho{
			Origem:         req.Rota[i],
			Destino:        req.Rota[i+1],
			AssentosLivres: req.AssentosTotais,
			Preco:          req.PrecoPorTrecho,
			Passageiros:    []string{},
		})
	}

	novaCarona := protocolo.Carona{
		ID:             ProximoCaronaID,
		Motorista:      req.Motorista,
		Data:           req.Data,
		Horario:        req.Horario,
		AssentosTotais: req.AssentosTotais,
		PrecoPorTrecho: req.PrecoPorTrecho,
		Rota:           req.Rota,
		Trechos:        trechos,
	}

	CaronasRegistradas = append(CaronasRegistradas, novaCarona)
	caronaID := ProximoCaronaID
	ProximoCaronaID++

	if err := SalvarJSON(ArquivoCaronas, CaronasRegistradas); err != nil {
		fmt.Printf("[ERRO] Falha ao persistir caronas: %v\n", err)
	}

	fmt.Printf("[CARONA] Nova carona #%d cadastrada por '%s' (%s -> %s)\n",
		caronaID, req.Motorista, req.Rota[0], req.Rota[len(req.Rota)-1])

	responderPublicacao(conn, true, "Carona publicada com sucesso", caronaID)
}

func responderPublicacao(conn net.Conn, sucesso bool, mensagem string, id int) {
	resp := protocolo.RespostaPublicarCarona{
		Tipo:     protocolo.TipoPublicarCaronaRes,
		Sucesso:  sucesso,
		Mensagem: mensagem,
		CaronaID: id,
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func ProcessarConsultarCaronas(conn net.Conn, dadosBrutos string) {
	var req protocolo.ConsultaCaronasMotorista
	if err := json.Unmarshal([]byte(dadosBrutos), &req); err != nil {
		return
	}

	MutexCaronas.RLock()
	defer MutexCaronas.RUnlock()

	var minhas []protocolo.Carona
	for _, c := range CaronasRegistradas {
		if c.Motorista == req.Motorista {
			minhas = append(minhas, c)
		}
	}

	resp := protocolo.RespostaConsultarCaronas{
		Tipo:     protocolo.TipoConsultarCaronasRes,
		Sucesso:  true,
		Mensagem: "Consulta concluida",
		Caronas:  minhas,
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func ProcessarCancelarCarona(conn net.Conn, dadosBrutos string) {
	var req protocolo.CancelamentoCarona
	if err := json.Unmarshal([]byte(dadosBrutos), &req); err != nil {
		return
	}

	MutexCaronas.Lock()
	defer MutexCaronas.Unlock()

	idx := -1
	for i, c := range CaronasRegistradas {
		if c.ID == req.CaronaID && c.Motorista == req.Motorista {
			idx = i
			break
		}
	}

	if idx == -1 {
		resp := protocolo.RespostaSimples{
			Tipo:     protocolo.TipoCancelarCaronaRes,
			Sucesso:  false,
			Mensagem: "Carona nao encontrada ou permissao negada",
		}
		_ = json.NewEncoder(conn).Encode(resp)
		return
	}

	CaronasRegistradas = append(CaronasRegistradas[:idx], CaronasRegistradas[idx+1:]...)

	if AoCancelarCarona != nil {
		go AoCancelarCarona(req.CaronaID, req.Motorista)
	}

	if err := SalvarJSON(ArquivoCaronas, CaronasRegistradas); err != nil {
		fmt.Printf("[ERRO] Falha ao persistir cancelamento de carona: %v\n", err)
	}

	fmt.Printf("[CARONA] Carona #%d cancelada pelo motorista '%s'\n", req.CaronaID, req.Motorista)

	resp := protocolo.RespostaSimples{
		Tipo:     protocolo.TipoCancelarCaronaRes,
		Sucesso:  true,
		Mensagem: "Carona cancelada com sucesso",
	}
	_ = json.NewEncoder(conn).Encode(resp)
}
