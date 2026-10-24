package distribuido

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

type EstadoNo string

const (
	NoAtivo   EstadoNo = "ATIVO"
	NoInativo EstadoNo = "INATIVO"
)

type infoParceiro struct {
	conn        net.Conn
	estado      EstadoNo
	ultimoVisto time.Time
}

type RicartAgrawala struct {
	idEmpresa string
	mu        sync.Mutex

	relogio int64

	solicitandoCS bool
	executandoCS  bool
	timestampReq  int64
	recursoAtual  string

	// Controle de votos e quórum
	votosEsperados   map[string]bool // Nós ativos de quem ainda precisamos de REPLY
	canalLiberacaoCS chan struct{}

	respostasPostergadas []string

	parceiros map[string]*infoParceiro

	// Parâmetros do Detector de Falhas
	intervaloHeartbeat time.Duration
	timeoutInatividade time.Duration
}

func NovoRicartAgrawala(idEmpresa string) *RicartAgrawala {
	ra := &RicartAgrawala{
		idEmpresa:            idEmpresa,
		canalLiberacaoCS:     make(chan struct{}, 1),
		respostasPostergadas: make([]string, 0),
		parceiros:            make(map[string]*infoParceiro),
		votosEsperados:       make(map[string]bool),
		intervaloHeartbeat:   1 * time.Second,
		timeoutInatividade:   3 * time.Second,
	}

	// Inicia a goroutine de monitoramento contínuo
	go ra.iniciarDetectorFalhas()

	return ra
}

func (ra *RicartAgrawala) RegistrarParceiro(idParceiro string, conn net.Conn) {
	ra.mu.Lock()
	ra.parceiros[idParceiro] = &infoParceiro{
		conn:        conn,
		estado:      NoAtivo,
		ultimoVisto: time.Now(),
	}
	ra.mu.Unlock()

	go ra.dispatcherEventos(idParceiro, conn)
}

// iniciarDetectorFalhas envia PING periodicamente e marca nós sem resposta como INATIVO
func (ra *RicartAgrawala) iniciarDetectorFalhas() {
	ticker := time.NewTicker(ra.intervaloHeartbeat)
	defer ticker.Stop()

	for range ticker.C {
		ra.mu.Lock()
		agora := time.Now()

		for id, info := range ra.parceiros {
			// 1. Envia PING de batimento cardíaco
			go ra.enviarMensagemAssincrona(id, MensagemDistribuida{
				Tipo:      MsgPing,
				OrigemID:  ra.idEmpresa,
				Timestamp: ra.relogio,
			})

			// 2. Verifica se o nó parou de responder
			if info.estado == NoAtivo && agora.Sub(info.ultimoVisto) > ra.timeoutInatividade {
				info.estado = NoInativo
				fmt.Printf("[Ricart-Agrawala] AVISO: Nó '%s' marcado como INATIVO (sem resposta há %v)\n", id, ra.timeoutInatividade)

				// Se estávamos esperando o voto desse nó que caiu, removemos a exigência
				if ra.solicitandoCS && ra.votosEsperados[id] {
					delete(ra.votosEsperados, id)
					ra.verificarConcessaoCS()
				}
			}
		}
		ra.mu.Unlock()
	}
}

func (ra *RicartAgrawala) dispatcherEventos(idRemoto string, conn net.Conn) {
	leitor := bufio.NewReader(conn)

	for {
		linha, err := leitor.ReadBytes('\n')
		if err != nil {
			// Conexão TCP caiu
			ra.mu.Lock()
			if info, existe := ra.parceiros[idRemoto]; existe {
				info.estado = NoInativo
				if ra.solicitandoCS && ra.votosEsperados[idRemoto] {
					delete(ra.votosEsperados, idRemoto)
					ra.verificarConcessaoCS()
				}
			}
			ra.mu.Unlock()
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

	// Atualiza relógio lógico de Lamport
	if msg.Timestamp > ra.relogio {
		ra.relogio = msg.Timestamp
	}
	ra.relogio++

	// Atualiza última atividade recebida do nó (heartbeat)
	if info, existe := ra.parceiros[msg.OrigemID]; existe {
		info.ultimoVisto = time.Now()
		if info.estado == NoInativo {
			info.estado = NoAtivo
			fmt.Printf("[Ricart-Agrawala] INFO: Nó '%s' recuperado e marcado como ATIVO\n", msg.OrigemID)
		}
	}

	switch msg.Tipo {
	case MsgPing:
		// Responde com PONG imediatamente
		go ra.enviarMensagemAssincrona(msg.OrigemID, MensagemDistribuida{
			Tipo:      MsgPong,
			OrigemID:  ra.idEmpresa,
			Timestamp: ra.relogio,
		})

	case MsgPong:
		// Apenas atualizou ultimoVisto acima

	case MsgReply:
		if ra.solicitandoCS && ra.recursoAtual == msg.RecursoID {
			delete(ra.votosEsperados, msg.OrigemID)
			ra.verificarConcessaoCS()
		}

	case MsgRequest:
		devePostergar := false

		if ra.executandoCS && ra.recursoAtual == msg.RecursoID {
			devePostergar = true
		} else if ra.solicitandoCS && ra.recursoAtual == msg.RecursoID {
			// Regra de desempate de Lamport: menor timestamp vence; em empate, menor ID
			if ra.timestampReq < msg.Timestamp || (ra.timestampReq == msg.Timestamp && ra.idEmpresa < msg.OrigemID) {
				devePostergar = true
			}
		}

		if devePostergar {
			ra.respostasPostergadas = append(ra.respostasPostergadas, msg.OrigemID)
		} else {
			go ra.enviarMensagemAssincrona(msg.OrigemID, MensagemDistribuida{
				Tipo:      MsgReply,
				OrigemID:  ra.idEmpresa,
				Timestamp: ra.relogio,
				RecursoID: msg.RecursoID,
			})
		}
	}
}

// verificarConcessaoCS checa se todos os votos da maioria viva foram recolhidos
func (ra *RicartAgrawala) verificarConcessaoCS() {
	if len(ra.votosEsperados) == 0 && ra.solicitandoCS {
		select {
		case ra.canalLiberacaoCS <- struct{}{}:
		default:
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

	ra.votosEsperados = make(map[string]bool)
	totalNosCadastrados := len(ra.parceiros) + 1 // +1 contando nó local
	nosAtivos := 1

	msgReq := MensagemDistribuida{
		Tipo:      MsgRequest,
		OrigemID:  ra.idEmpresa,
		Timestamp: ra.timestampReq,
		RecursoID: recursoID,
	}

	for idDestino, info := range ra.parceiros {
		// Apenas solicitamos voto e aguardamos de nós considerados ATIVOS
		if info.estado == NoAtivo {
			ra.votosEsperados[idDestino] = true
			nosAtivos++
			go ra.enviarMensagemAssincrona(idDestino, msgReq)
		}
	}

	// Verificação de Quórum Mínimo: nós ativos devem ser estritamente > N/2
	quoromMinimo := (totalNosCadastrados / 2) + 1
	if nosAtivos < quoromMinimo {
		ra.solicitandoCS = false
		ra.mu.Unlock()
		return fmt.Errorf("quorum insuficiente (%d/%d nós ativos) para disputar %s com seguranca", nosAtivos, totalNosCadastrados, recursoID)
	}

	// Se não há parceiros ativos esperando resposta, entra direto
	if len(ra.votosEsperados) == 0 {
		ra.solicitandoCS = false
		ra.executandoCS = true
		ra.mu.Unlock()
		return nil
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
		return fmt.Errorf("timeout (%v) ao disputar recurso %s via ricart-agrawala", timeout, recursoID)
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
	info, ok := ra.parceiros[idDestino]
	ra.mu.Unlock()

	if !ok || info.conn == nil {
		return
	}

	bytesDados, err := json.Marshal(msg)
	if err != nil {
		return
	}
	bytesDados = append(bytesDados, '\n')

	_ = info.conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
	_, _ = info.conn.Write(bytesDados)
}
