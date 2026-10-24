package servicos

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

var (
	ErrReservaNaoEncontrada = errors.New("reserva nao localizada")
)

type GerenciadorReservas struct {
	idEmpresa           string
	caminhoReservas     string
	caminhoNotificacoes string
	mu                  sync.RWMutex

	reservas      map[int]modelos.Reserva          // ReservaID -> Reserva
	notificacoes  map[string][]modelos.Notificacao // Passageiro -> lista de notificacoes
	proximoID     int
	cancelamentos []modelos.TrechoViagem // histórico de trechos cancelados
}

func NovoGerenciadorReservas(idEmpresa, diretorioDados string) *GerenciadorReservas {
	caminhoRes := filepath.Join(diretorioDados, "reservas.json")
	caminhoNot := filepath.Join(diretorioDados, "notificacoes.json")

	gr := &GerenciadorReservas{
		idEmpresa:           idEmpresa,
		caminhoReservas:     caminhoRes,
		caminhoNotificacoes: caminhoNot,
		reservas:            make(map[int]modelos.Reserva),
		notificacoes:        make(map[string][]modelos.Notificacao),
		proximoID:           1,
	}

	_ = gr.carregarDoDisco()
	return gr
}

func (gr *GerenciadorReservas) CriarReservaConfirmada(txID, passageiro string, itinerario modelos.Itinerario) modelos.Reserva {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	reserva := modelos.Reserva{
		ID:         gr.proximoID,
		TxID:       txID,
		Passageiro: passageiro,
		Data:       itinerario.Data,
		PrecoTotal: itinerario.PrecoTotal,
		Trechos:    itinerario.Trechos,
	}

	gr.reservas[gr.proximoID] = reserva
	gr.proximoID++

	msg := fmt.Sprintf("Itinerario confirmado com sucesso (TxID: %s, %d trechos). Boa viagem!", txID, len(itinerario.Trechos))
	gr.adicionarNotificacaoSemLock(passageiro, msg)

	_ = gr.salvarEmDisco()
	return reserva
}

func (gr *GerenciadorReservas) ConsultarReservasPassageiro(passageiro string) []modelos.Reserva {
	gr.mu.RLock()
	defer gr.mu.RUnlock()

	var lista []modelos.Reserva
	for _, r := range gr.reservas {
		if r.Passageiro == passageiro {
			lista = append(lista, r)
		}
	}
	return lista
}

func (gr *GerenciadorReservas) CancelarReserva(reservaID int, passageiro string) (*modelos.Reserva, error) {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	res, existe := gr.reservas[reservaID]
	if !existe || res.Passageiro != passageiro {
		return nil, ErrReservaNaoEncontrada
	}

	// Guarda os trechos cancelados para responder ao que caiu
	for _, t := range res.Trechos {
		gr.cancelamentos = append(gr.cancelamentos, t)
	}

	delete(gr.reservas, reservaID)
	_ = gr.salvarEmDisco()

	return &res, nil
}

// VerificarTrechosCancelados identifica quais trechos de uma empresa já foram cancelados
func (gr *GerenciadorReservas) VerificarTrechosCancelados(empresaAlvo string, ativos []modelos.TrechoOcupadoInfo) []modelos.TrechoOcupadoInfo {
	gr.mu.RLock()
	defer gr.mu.RUnlock()

	var paraLiberar []modelos.TrechoOcupadoInfo

	for _, item := range ativos {
		for _, canc := range gr.cancelamentos {
			if strings.EqualFold(canc.Empresa, empresaAlvo) &&
				canc.CaronaID == item.CaronaID &&
				strings.EqualFold(canc.Origem, item.Origem) &&
				strings.EqualFold(canc.Destino, item.Destino) {
				paraLiberar = append(paraLiberar, item)
				break
			}
		}
	}

	return paraLiberar
}

func (gr *GerenciadorReservas) AdicionarNotificacao(passageiro, mensagem string) {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	gr.adicionarNotificacaoSemLock(passageiro, mensagem)
	_ = gr.salvarNotificacoesEmDisco()
}

func (gr *GerenciadorReservas) adicionarNotificacaoSemLock(passageiro, mensagem string) {
	nova := modelos.Notificacao{
		Passageiro: passageiro,
		Mensagem:   mensagem,
		Data:       time.Now().Format("2006-01-02 15:04:05"),
	}
	gr.notificacoes[passageiro] = append(gr.notificacoes[passageiro], nova)
}

func (gr *GerenciadorReservas) ConsultarNotificacoes(passageiro string) []modelos.Notificacao {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	mensagens, existe := gr.notificacoes[passageiro]
	if !existe {
		return []modelos.Notificacao{}
	}

	delete(gr.notificacoes, passageiro)
	_ = gr.salvarNotificacoesEmDisco()

	return mensagens
}

func (gr *GerenciadorReservas) salvarEmDisco() error {
	bytesDados, err := json.MarshalIndent(gr.reservas, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(gr.caminhoReservas, bytesDados, 0644)
}

func (gr *GerenciadorReservas) salvarNotificacoesEmDisco() error {
	bytesDados, err := json.MarshalIndent(gr.notificacoes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(gr.caminhoNotificacoes, bytesDados, 0644)
}

func (gr *GerenciadorReservas) carregarDoDisco() error {
	conteudoRes, err := os.ReadFile(gr.caminhoReservas)
	if err == nil && len(conteudoRes) > 0 {
		_ = json.Unmarshal(conteudoRes, &gr.reservas)
		for id := range gr.reservas {
			if id >= gr.proximoID {
				gr.proximoID = id + 1
			}
		}
	}

	conteudoNot, err := os.ReadFile(gr.caminhoNotificacoes)
	if err == nil && len(conteudoNot) > 0 {
		_ = json.Unmarshal(conteudoNot, &gr.notificacoes)
	}

	return nil
}
