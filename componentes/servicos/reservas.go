package servicos

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

var (
	ErrReservaNaoEncontrada = errors.New("reserva nao localizada")
)

// GerenciadorReservas controla as reservas confirmadas e notificacoes aos passageiros
type GerenciadorReservas struct {
	idEmpresa           string
	caminhoReservas     string
	caminhoNotificacoes string
	mu                  sync.RWMutex

	reservas     map[int]modelos.Reserva          // ReservaID -> Reserva
	notificacoes map[string][]modelos.Notificacao // Passageiro -> lista de notificacoes
	proximoID    int
}

// NovoGerenciadorReservas inicializa a memoria e restaura os bilhetes ja gravados em disco
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

// CriarReservaConfirmada armazena o bilhete definitivo apos o GLOBAL COMMIT do 2PC
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

	// Emite aviso interno para a caixa de notificacoes do passageiro
	msg := fmt.Sprintf("Itinerario confirmado com sucesso (TxID: %s, %d trechos). Boa viagem!", txID, len(itinerario.Trechos))
	gr.adicionarNotificacaoSemLock(passageiro, msg)

	_ = gr.salvarEmDisco()
	return reserva
}

// ConsultarReservasPassageiro lista todos os bilhetes emitidos para o passageiro conectado
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

// CancelarReserva remove a reserva ativa e sinaliza o cancelamento
func (gr *GerenciadorReservas) CancelarReserva(reservaID int, passageiro string) (*modelos.Reserva, error) {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	res, existe := gr.reservas[reservaID]
	if !existe || res.Passageiro != passageiro {
		return nil, ErrReservaNaoEncontrada
	}

	delete(gr.reservas, reservaID)
	_ = gr.salvarEmDisco()

	return &res, nil
}

// AdicionarNotificacao insere uma nova mensagem para um passageiro
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

// ConsultarNotificacoes devolve os avisos do passageiro e esvazia a caixa pendente
func (gr *GerenciadorReservas) ConsultarNotificacoes(passageiro string) []modelos.Notificacao {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	mensagens, existe := gr.notificacoes[passageiro]
	if !existe {
		return []modelos.Notificacao{}
	}

	// Limpa apos a leitura pelo cliente
	delete(gr.notificacoes, passageiro)
	_ = gr.salvarNotificacoesEmDisco()

	return mensagens
}

// Persistência em disco JSON
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
	// 1. Carrega Reservas
	conteudoRes, err := os.ReadFile(gr.caminhoReservas)
	if err == nil && len(conteudoRes) > 0 {
		_ = json.Unmarshal(conteudoRes, &gr.reservas)
		for id := range gr.reservas {
			if id >= gr.proximoID {
				gr.proximoID = id + 1
			}
		}
	}

	// 2. Carrega Notificações
	conteudoNot, err := os.ReadFile(gr.caminhoNotificacoes)
	if err == nil && len(conteudoNot) > 0 {
		_ = json.Unmarshal(conteudoNot, &gr.notificacoes)
	}

	return nil
}
