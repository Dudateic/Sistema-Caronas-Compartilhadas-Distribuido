package distribuido

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

var (
	ErrTransacaoNaoEncontrada = errors.New("transacao nao localizada")
	ErrEstadoInvalido         = errors.New("transicao de estado invalida para o 2pc")
)

type ServicoAssentosLocal interface {
	BloquearAssentoProvisorio(caronaID int, origem, destino, passageiro string) error
	LiberarAssentoProvisorio(caronaID int, origem, destino, passageiro string) error
	ConfirmarAssentoDefinitivo(caronaID int, origem, destino, passageiro string) error
}

// ReconciliadorAssentos é implementado opcionalmente pelo serviço de caronas.
// Após a recuperação, recalcula AssentosLivres = totais - passageiros - pendentes,
// o que repara qualquer janela entre "assento decrementado" e "voto gravado no log".
type ReconciliadorAssentos interface {
	ReconciliarAssentos(pendentes map[string]int)
}

// ConfirmadorIdempotente confirma o assento somente se o passageiro ainda não consta no trecho.
// É usado ao reaplicar um COMMIT durante a recuperação, quando a confirmação pode já ter sido gravada.
type ConfirmadorIdempotente interface {
	ConfirmarAssentoSeAusente(caronaID int, origem, destino, passageiro string) error
}

// ChaveTrecho identifica um trecho de uma carona nesta empresa.
func ChaveTrecho(caronaID int, origem, destino string) string {
	return fmt.Sprintf("%d|%s|%s", caronaID, strings.ToUpper(strings.TrimSpace(origem)), strings.ToUpper(strings.TrimSpace(destino)))
}

// ConsultaDecisao pergunta ao coordenador (pelo nome da empresa) o destino de uma transação.
type ConsultaDecisao func(coordenador, txID string) (string, error)

// ConsultaEstadoPar pergunta a outra empresa participante em que estado ela está na transação.
type ConsultaEstadoPar func(empresa, txID string) (string, error)

type Participante2PC struct {
	idEmpresa   string
	servico     ServicoAssentosLocal
	wal         *LogTransacoes
	relogio     *RelogioLamport
	consulta    ConsultaDecisao
	consultaPar ConsultaEstadoPar
	mu          sync.Mutex

	// Transações com voto SIM e sem decisão ainda: TxID -> trechos preparados
	transacoes map[string][]*TransacaoLocal
	// Transações já decididas (CONFIRMADA ou ABORTADA), para tornar commit/abort/prepare idempotentes
	decididas map[string]modelos.EstadoTransacao
}

type TransacaoLocal struct {
	TxID        string
	Estado      modelos.EstadoTransacao
	CaronaID    int
	Origem      string
	Destino     string
	Passageiro  string
	RecursoID   string
	Coordenador string
	// Participantes são as empresas da transação (para a terminação cooperativa)
	Participantes []string
	Timestamp     time.Time
}

// NovoParticipante2PC inicializa a cohorte e recupera o estado persistido no log
func NovoParticipante2PC(idEmpresa string, servico ServicoAssentosLocal, wal *LogTransacoes) (*Participante2PC, error) {
	p := &Participante2PC{
		idEmpresa:  idEmpresa,
		servico:    servico,
		wal:        wal,
		relogio:    &RelogioLamport{},
		transacoes: make(map[string][]*TransacaoLocal),
		decididas:  make(map[string]modelos.EstadoTransacao),
	}

	if err := p.RecuperarAposFalha(); err != nil {
		return nil, fmt.Errorf("falha ao recuperar transacoes do disco: %w", err)
	}
	return p, nil
}

// DefinirConsultaDecisao configura como perguntar a decisão ao coordenador.
func (p *Participante2PC) DefinirConsultaDecisao(c ConsultaDecisao) {
	p.mu.Lock()
	p.consulta = c
	p.mu.Unlock()
}

// DefinirConsultaPares configura a terminação cooperativa: como perguntar o estado
// de uma transação às outras empresas participantes quando o coordenador não responde.
func (p *Participante2PC) DefinirConsultaPares(c ConsultaEstadoPar) {
	p.mu.Lock()
	p.consultaPar = c
	p.mu.Unlock()
}

// ConsultarEstado informa o estado local de uma transação a outro participante.
//
// Se a transação é DESCONHECIDA aqui, este participante nunca votou SIM nela. Como o
// coordenador só decide COMMIT com o voto SIM de todos, ele jamais poderá confirmá-la:
// registramos o ABORT agora (e portanto recusaremos qualquer PREPARE que chegue atrasado),
// o que torna seguro para quem perguntou abortar também.
func (p *Participante2PC) ConsultarEstado(txID string) string {
	p.mu.Lock()
	if estado, ok := p.decididas[txID]; ok {
		p.mu.Unlock()
		if estado == modelos.TxConfirmada {
			return modelos.EstadoConfirmada
		}
		return modelos.EstadoAbortada
	}
	if len(p.transacoes[txID]) > 0 {
		p.mu.Unlock()
		return modelos.EstadoPreparada
	}
	p.mu.Unlock()

	if err := p.ProcessarAbort(txID); err != nil {
		return modelos.EstadoDesconhecida // não conseguiu registrar a promessa: não afirma nada
	}
	return modelos.EstadoAbortada
}

// Relogio expõe o relógio lógico desta empresa.
func (p *Participante2PC) Relogio() *RelogioLamport { return p.relogio }

func recursoIDDe(caronaID int, origem, destino string) string {
	return fmt.Sprintf("carona_%d_%s_%s", caronaID, origem, destino)
}

func (p *Participante2PC) gravar(txID string, tipo TipoRegistroLog, recurso, detalhes string) error {
	if p.wal == nil {
		return nil
	}
	return p.wal.Gravar(txID, tipo, recurso, detalhes)
}

// ProcessarPrepare bloqueia o assento, persiste o voto SIM no log e responde.
// É idempotente: repetir o mesmo PREPARE não bloqueia um segundo assento.
func (p *Participante2PC) ProcessarPrepare(req modelos.PrepareRequest) (modelos.PrepareResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ts := p.relogio.Atualizar(req.Timestamp)
	resposta := func(sim bool, motivo string) modelos.PrepareResponse {
		return modelos.PrepareResponse{TxID: req.TxID, VotoSim: sim, Motivo: motivo, Timestamp: ts}
	}

	// A transação já foi decidida: um PREPARE atrasado não pode voltar a bloquear assentos.
	if estado, ok := p.decididas[req.TxID]; ok {
		if estado == modelos.TxConfirmada {
			return resposta(true, ""), nil
		}
		return resposta(false, "transacao ja abortada"), nil
	}

	for _, t := range p.transacoes[req.TxID] {
		if t.CaronaID == req.CaronaID && strings.EqualFold(t.Origem, req.Origem) &&
			strings.EqualFold(t.Destino, req.Destino) && strings.EqualFold(t.Passageiro, req.Passageiro) {
			return resposta(true, ""), nil // repetição do mesmo PREPARE
		}
	}

	if err := p.servico.BloquearAssentoProvisorio(req.CaronaID, req.Origem, req.Destino, req.Passageiro); err != nil {
		return resposta(false, fmt.Sprintf("assento indisponivel: %v", err)), nil
	}

	recursoID := recursoIDDe(req.CaronaID, req.Origem, req.Destino)
	detalhes, _ := json.Marshal(req)
	if errWal := p.gravar(req.TxID, LogPrepare, recursoID, string(detalhes)); errWal != nil {
		_ = p.servico.LiberarAssentoProvisorio(req.CaronaID, req.Origem, req.Destino, req.Passageiro)
		return resposta(false, "erro ao sincronizar log de transacao no disco"), errWal
	}

	p.transacoes[req.TxID] = append(p.transacoes[req.TxID], &TransacaoLocal{
		TxID:          req.TxID,
		Estado:        modelos.TxPreparada,
		CaronaID:      req.CaronaID,
		Origem:        req.Origem,
		Destino:       req.Destino,
		Passageiro:    req.Passageiro,
		RecursoID:     recursoID,
		Coordenador:   req.Coordenador,
		Participantes: req.Participantes,
		Timestamp:     time.Now(),
	})

	return resposta(true, ""), nil
}

// ProcessarCommit grava a decisão no log e só então efetiva os assentos. Idempotente.
func (p *Participante2PC) ProcessarCommit(txID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if estado, ok := p.decididas[txID]; ok {
		if estado == modelos.TxConfirmada {
			return nil
		}
		return fmt.Errorf("%w: transacao ja abortada", ErrEstadoInvalido)
	}

	lista, existe := p.transacoes[txID]
	if !existe || len(lista) == 0 {
		return ErrTransacaoNaoEncontrada
	}

	if err := p.gravar(txID, LogCommit, "", "commit_efetivado"); err != nil {
		return err // continua preparada; o coordenador repete o COMMIT
	}

	for _, tx := range lista {
		_ = p.servico.ConfirmarAssentoDefinitivo(tx.CaronaID, tx.Origem, tx.Destino, tx.Passageiro)
		tx.Estado = modelos.TxConfirmada
	}
	_ = p.gravar(txID, LogFim, "", "commit_aplicado")

	p.decididas[txID] = modelos.TxConfirmada
	delete(p.transacoes, txID)
	return nil
}

// ProcessarAbort libera os assentos bloqueados. Idempotente. Se a transação é
// desconhecida, registra o ABORT para recusar um PREPARE que chegue atrasado.
func (p *Participante2PC) ProcessarAbort(txID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if estado, ok := p.decididas[txID]; ok {
		if estado == modelos.TxAbortada {
			return nil
		}
		return fmt.Errorf("%w: transacao ja confirmada", ErrEstadoInvalido)
	}

	lista := p.transacoes[txID]
	if err := p.gravar(txID, LogAbort, "", "abort_solicitado"); err != nil {
		return err
	}

	for _, tx := range lista {
		_ = p.servico.LiberarAssentoProvisorio(tx.CaronaID, tx.Origem, tx.Destino, tx.Passageiro)
		tx.Estado = modelos.TxAbortada
	}
	_ = p.gravar(txID, LogFim, "", "abort_aplicado")

	p.decididas[txID] = modelos.TxAbortada
	delete(p.transacoes, txID)
	return nil
}

// RecuperarAposFalha reconstrói o estado a partir do log:
//   - PREPARE sem decisão  -> transação pendente (assento segue bloqueado até saber a decisão)
//   - COMMIT sem FIM       -> reaplica a confirmação (idempotente)
//   - ABORT sem FIM        -> reaplica a liberação
//
// Por fim reconcilia AssentosLivres com o que o log diz estar pendente.
func (p *Participante2PC) RecuperarAposFalha() error {
	if p.wal == nil {
		return nil
	}

	entradas, err := p.wal.Entradas()
	if err != nil {
		return err
	}

	type estadoTx struct {
		prepares []modelos.PrepareRequest
		commit   bool
		abort    bool
		fim      bool
	}
	porTx := map[string]*estadoTx{}
	var ordem []string
	for _, e := range entradas {
		st, ok := porTx[e.TxID]
		if !ok {
			st = &estadoTx{}
			porTx[e.TxID] = st
			ordem = append(ordem, e.TxID)
		}
		switch e.Tipo {
		case LogPrepare:
			var req modelos.PrepareRequest
			if json.Unmarshal([]byte(e.Detalhes), &req) == nil {
				st.prepares = append(st.prepares, req)
			}
		case LogCommit:
			st.commit = true
		case LogAbort:
			st.abort = true
		case LogFim:
			st.fim = true
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	pendentes := map[string]int{}
	for _, txID := range ordem {
		st := porTx[txID]
		switch {
		case st.commit:
			if !st.fim {
				for _, req := range st.prepares {
					if idem, ok := p.servico.(ConfirmadorIdempotente); ok {
						_ = idem.ConfirmarAssentoSeAusente(req.CaronaID, req.Origem, req.Destino, req.Passageiro)
					} else {
						_ = p.servico.ConfirmarAssentoDefinitivo(req.CaronaID, req.Origem, req.Destino, req.Passageiro)
					}
				}
				_ = p.wal.Gravar(txID, LogFim, "", "commit_reaplicado_na_recuperacao")
			}
			p.decididas[txID] = modelos.TxConfirmada
		case st.abort:
			if !st.fim {
				_ = p.wal.Gravar(txID, LogFim, "", "abort_reaplicado_na_recuperacao")
			}
			p.decididas[txID] = modelos.TxAbortada
		case len(st.prepares) > 0:
			for _, req := range st.prepares {
				p.transacoes[txID] = append(p.transacoes[txID], &TransacaoLocal{
					TxID:          txID,
					Estado:        modelos.TxPreparada,
					CaronaID:      req.CaronaID,
					Origem:        req.Origem,
					Destino:       req.Destino,
					Passageiro:    req.Passageiro,
					RecursoID:     recursoIDDe(req.CaronaID, req.Origem, req.Destino),
					Coordenador:   req.Coordenador,
					Participantes: req.Participantes,
					Timestamp:     time.Now(),
				})
				pendentes[ChaveTrecho(req.CaronaID, req.Origem, req.Destino)]++
			}
		}
	}

	if rec, ok := p.servico.(ReconciliadorAssentos); ok {
		rec.ReconciliarAssentos(pendentes)
	}
	return nil
}

// LimparTransacoesExpiradas resolve preparações que passaram do tempo limite.
// Um participante que votou SIM não pode abortar sozinho (o coordenador pode ter
// decidido COMMIT), então segue a terminação em duas etapas:
//
//  1. pergunta a decisão ao coordenador: COMMIT -> confirma | ABORT -> libera |
//     EM_ANDAMENTO -> espera o próximo ciclo;
//  2. se o coordenador está inalcançável, pergunta às OUTRAS empresas participantes
//     (terminação cooperativa): qualquer uma CONFIRMADA -> confirma; qualquer uma
//     ABORTADA (inclui as que nunca votaram) -> libera; todas PREPARADAS ou
//     inalcançáveis -> continua bloqueado até alguém saber a decisão.
//
// Sem função de consulta configurada, aborta direto (uso em testes unitários).
func (p *Participante2PC) LimparTransacoesExpiradas(timeout time.Duration) {
	type candidato struct {
		txID, coordenador string
		participantes     []string
	}

	p.mu.Lock()
	consulta := p.consulta
	consultaPar := p.consultaPar
	agora := time.Now()
	var expiradas []candidato
	for txID, lista := range p.transacoes {
		for _, tx := range lista {
			if tx.Estado == modelos.TxPreparada && agora.Sub(tx.Timestamp) > timeout {
				expiradas = append(expiradas, candidato{txID, tx.Coordenador, tx.Participantes})
				break
			}
		}
	}
	p.mu.Unlock()

	for _, c := range expiradas {
		if consulta == nil || c.coordenador == "" {
			_ = p.ProcessarAbort(c.txID)
			continue
		}
		decisao, err := consulta(c.coordenador, c.txID)
		if err == nil {
			switch decisao {
			case modelos.DecisaoCommit:
				_ = p.ProcessarCommit(c.txID)
			case modelos.DecisaoAbort:
				_ = p.ProcessarAbort(c.txID)
			}
			continue
		}
		p.terminacaoCooperativa(c.txID, c.coordenador, c.participantes, consultaPar)
	}
}

// terminacaoCooperativa tenta decidir a transação perguntando aos demais participantes.
func (p *Participante2PC) terminacaoCooperativa(txID, coordenador string, participantes []string, consultaPar ConsultaEstadoPar) {
	if consultaPar == nil {
		return
	}
	for _, outro := range participantes {
		if strings.EqualFold(outro, p.idEmpresa) || strings.EqualFold(outro, coordenador) {
			continue
		}
		estado, err := consultaPar(outro, txID)
		if err != nil {
			continue
		}
		switch estado {
		case modelos.EstadoConfirmada:
			_ = p.ProcessarCommit(txID)
			return
		case modelos.EstadoAbortada:
			_ = p.ProcessarAbort(txID)
			return
		}
	}
}

// PendentesAtivas devolve quantas transações aguardam decisão (usado em testes e métricas).
func (p *Participante2PC) PendentesAtivas() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.transacoes)
}
