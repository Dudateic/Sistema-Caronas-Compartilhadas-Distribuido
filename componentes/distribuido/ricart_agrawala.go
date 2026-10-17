package distribuido

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type RicartAgrawala struct {
	idEmpresa string
	mu        sync.Mutex

	relogio int64

	solicitandoCS bool
	executandoCS  bool
	timestampReq  int64
	recursoAtual  string

	votosNecessarios int
	votosRecebidos   int32
	canalLiberacaoCS chan struct{}

	respostasPostergadas []string // IDs dos nós que estão esperando meu REPLY

	conexoes map[string]net.Conn
}

func NovoRicartAgrawala(idEmpresa string) *RicartAgrawala {
	return &RicartAgrawala{
		idEmpresa:            idEmpresa,
		canalLiberacaoCS:     make(chan struct{}, 1),
		respostasPostergadas: make([]string, 0),
		conexoes:             make(map[string]net.Conn),
	}
}

func (ra *RicartAgrawala) RegistrarParceiro(idParceiro string, conn net.Conn) {
	ra.mu.Lock()
	ra.conexoes[idParceiro] = conn
	ra.votosNecessarios = len(ra.conexoes)
	ra.mu.Unlock()

	go ra.dispatcherEventos(idParceiro, conn)
}

func (ra *RicartAgrawala) dispatcherEventos(idRemoto string, conn net.Conn) {
	leitor := bufio.NewReader(conn)

	for {
		linha, err := leitor.ReadBytes('\n')
		if err != nil {
			return
		}

		var msg MensagemDistribuida
		if err := json.Unmarshal(linha, &msg); err != nil {
			continue
		}

		ra.processarMensagem(msg)
	}
}

func (ra *RicartAgrawala) processarMensagem(msg MensagemDistribuida) {
	ra.mu.Lock()
	defer ra.mu.Unlock()

	if msg.Timestamp > ra.relogio {
		ra.relogio = msg.Timestamp
	}
	ra.relogio++

	switch msg.Tipo {
	case MsgReply:
		recebidos := atomic.AddInt32(&ra.votosRecebidos, 1)
		if int(recebidos) >= ra.votosNecessarios && ra.solicitandoCS {
			select {
			case ra.canalLiberacaoCS <- struct{}{}:
			default:
			}
		}

	case MsgRequest:
		devePostergar := false

		if ra.executandoCS && ra.recursoAtual == msg.RecursoID {
			devePostergar = true
		} else if ra.solicitandoCS && ra.recursoAtual == msg.RecursoID {
			if ra.timestampReq < msg.Timestamp || (ra.timestampReq == msg.Timestamp && ra.idEmpresa < msg.OrigemID) {
				devePostergar = true
			}
		}

		if devePostergar {
			ra.respostasPostergadas = append(ra.respostasPostergadas, msg.OrigemID)
		} else {
			ra.enviarMensagemAssincrona(msg.OrigemID, MensagemDistribuida{
				Tipo:      MsgReply,
				OrigemID:  ra.idEmpresa,
				Timestamp: ra.relogio,
				RecursoID: msg.RecursoID,
			})
		}
	}
}

func (ra *RicartAgrawala) EntrarSecaoCritica(recursoID string, timeout time.Duration) error {
	ra.mu.Lock()
	ra.relogio++
	ra.solicitandoCS = true
	ra.executandoCS = false
	ra.timestampReq = ra.relogio
	ra.recursoAtual = recursoID
	atomic.StoreInt32(&ra.votosRecebidos, 0)

	if ra.votosNecessarios == 0 {
		ra.solicitandoCS = false
		ra.executandoCS = true
		ra.mu.Unlock()
		return nil
	}

	msgReq := MensagemDistribuida{
		Tipo:      MsgRequest,
		OrigemID:  ra.idEmpresa,
		Timestamp: ra.timestampReq,
		RecursoID: recursoID,
	}

	for idDestino := range ra.conexoes {
		go ra.enviarMensagemAssincrona(idDestino, msgReq)
	}
	ra.mu.Unlock()

	select {
	case <-ra.canalLiberacaoCS:
		ra.mu.Lock()
		ra.solicitandoCS = false
		ra.executandoCS = true
		ra.mu.Unlock()
		return nil

	case <-time.After(timeout):
		ra.mu.Lock()
		ra.solicitandoCS = false
		ra.executandoCS = false
		ra.mu.Unlock()
		return fmt.Errorf("timeout ao disputar recurso %s via ricart-agrawala", recursoID)
	}
}

func (ra *RicartAgrawala) SairSecaoCritica() {
	ra.mu.Lock()
	ra.executandoCS = false
	ra.solicitandoCS = false
	pendentes := ra.respostasPostergadas
	ra.respostasPostergadas = make([]string, 0)
	recurso := ra.recursoAtual
	ra.relogio++
	tsAtual := ra.relogio
	ra.mu.Unlock()

	for _, idDestino := range pendentes {
		go ra.enviarMensagemAssincrona(idDestino, MensagemDistribuida{
			Tipo:      MsgReply,
			OrigemID:  ra.idEmpresa,
			Timestamp: tsAtual,
			RecursoID: recurso,
		})
	}
}

func (ra *RicartAgrawala) enviarMensagemAssincrona(idDestino string, msg MensagemDistribuida) {
	ra.mu.Lock()
	conn, ok := ra.conexoes[idDestino]
	ra.mu.Unlock()

	if !ok || conn == nil {
		return
	}

	bytesDados, err := json.Marshal(msg)
	if err != nil {
		return
	}
	bytesDados = append(bytesDados, '\n')

	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write(bytesDados)
}
