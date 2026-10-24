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
	idEmpresa            string
	caminhoReservas      string
	caminhoNotificacoes  string
	caminhoCancelamentos string
	mu                   sync.RWMutex

	reservas      map[int]modelos.Reserva          // ReservaID -> Reserva
	notificacoes  map[string][]modelos.Notificacao // Passageiro -> lista de notificacoes
	proximoID     int
	cancelamentos []modelos.TrechoOcupadoInfo // Histórico persistido de trechos cancelados
}

func NovoGerenciadorReservas(idEmpresa, diretorioDados string) *GerenciadorReservas {
	caminhoRes := filepath.Join(diretorioDados, "reservas.json")
	caminhoNot := filepath.Join(diretorioDados, "notificacoes.json")
	caminhoCanc := filepath.Join(diretorioDados, "cancelamentos.json")

	gr := &GerenciadorReservas{
		idEmpresa:            idEmpresa,
		caminhoReservas:      caminhoRes,
		caminhoNotificacoes:  caminhoNot,
		caminhoCancelamentos: caminhoCanc,
		reservas:             make(map[int]modelos.Reserva),
		notificacoes:         make(map[string][]modelos.Notificacao),
		cancelamentos:        make([]modelos.TrechoOcupadoInfo, 0),
		proximoID:            1,
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
		if strings.EqualFold(r.Passageiro, passageiro) {
			lista = append(lista, r)
		}
	}
	return lista
}

func (gr *GerenciadorReservas) CancelarReserva(reservaID int, passageiro string) (*modelos.Reserva, error) {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	res, existe := gr.reservas[reservaID]
	if !existe || !strings.EqualFold(res.Passageiro, passageiro) {
		return nil, ErrReservaNaoEncontrada
	}

	// Guarda os trechos cancelados com o respectivo passageiro para reconciliação anti-entropia
	for _, t := range res.Trechos {
		gr.cancelamentos = append(gr.cancelamentos, modelos.TrechoOcupadoInfo{
			CaronaID:   t.CaronaID,
			Origem:     t.Origem,
			Destino:    t.Destino,
			Passageiro: res.Passageiro,
		})
	}

	delete(gr.reservas, reservaID)
	_ = gr.salvarEmDisco()
	_ = gr.salvarCancelamentosEmDisco()

	return &res, nil
}

// VerificarTrechosCancelados identifica se algum trecho reportado como ativo foi cancelado aqui
func (gr *GerenciadorReservas) VerificarTrechosCancelados(empresaAlvo string, ativos []modelos.TrechoOcupadoInfo) []modelos.TrechoOcupadoInfo {
	gr.mu.RLock()
	defer gr.mu.RUnlock()

	var paraLiberar []modelos.TrechoOcupadoInfo

	for _, item := range ativos {
		for _, canc := range gr.cancelamentos {
			// Compara CaronaID, Origem, Destino E o Passageiro para não cancelar a pessoa errada
			if canc.CaronaID == item.CaronaID &&
				strings.EqualFold(canc.Origem, item.Origem) &&
				strings.EqualFold(canc.Destino, item.Destino) &&
				strings.EqualFold(canc.Passageiro, item.Passageiro) {
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

	// Busca sem distinção de maiúsculas/minúsculas
	var mensagens []modelos.Notificacao
	var chavesParaRemover []string

	for p, msgs := range gr.notificacoes {
		if strings.EqualFold(p, passageiro) {
			mensagens = append(mensagens, msgs...)
			chavesParaRemover = append(chavesParaRemover, p)
		}
	}

	if len(mensagens) == 0 {
		return []modelos.Notificacao{}
	}

	for _, k := range chavesParaRemover {
		delete(gr.notificacoes, k)
	}
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

func (gr *GerenciadorReservas) salvarCancelamentosEmDisco() error {
	bytesDados, err := json.MarshalIndent(gr.cancelamentos, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(gr.caminhoCancelamentos, bytesDados, 0644)
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

	conteudoCanc, err := os.ReadFile(gr.caminhoCancelamentos)
	if err == nil && len(conteudoCanc) > 0 {
		_ = json.Unmarshal(conteudoCanc, &gr.cancelamentos)
	}

	return nil
}
