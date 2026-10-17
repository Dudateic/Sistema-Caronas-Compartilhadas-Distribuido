package distribuido

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

var (
	ErrTransacaoJaExiste      = errors.New("transacao ja registada neste participante")
	ErrTransacaoNaoEncontrada = errors.New("transacao nao localizada")
	ErrEstadoInvalido         = errors.New("transicao de estado invalida para o 2pc")
)

type ServicoAssentosLocal interface {
	BloquearAssentoProvisorio(caronaID int, origem, destino, passageiro string) error
	LiberarAssentoProvisorio(caronaID int, origem, destino, passageiro string) error
	ConfirmarAssentoDefinitivo(caronaID int, origem, destino, passageiro string) error
}

type Participante2PC struct {
	idEmpresa string
	servico   ServicoAssentosLocal
	wal       *LogTransacoes
	mu        sync.Mutex

	transacoes map[string]*TransacaoLocal
}

type TransacaoLocal struct {
	TxID       string
	Estado     modelos.EstadoTransacao
	CaronaID   int
	Origem     string
	Destino    string
	Passageiro string
	RecursoID  string
	Timestamp  time.Time
}

// NovoParticipante2PC inicializa a cohorte e recupera o estado persistido no log
func NovoParticipante2PC(idEmpresa string, servico ServicoAssentosLocal, wal *LogTransacoes) (*Participante2PC, error) {
	p := &Participante2PC{
		idEmpresa:  idEmpresa,
		servico:    servico,
		wal:        wal,
		transacoes: make(map[string]*TransacaoLocal),
	}

	if err := p.RecuperarAposFalha(); err != nil {
		return nil, fmt.Errorf("falha ao recuperar transacoes do disco: %w", err)
	}

	return p, nil
}

// ProcessarPrepare avalia o pedido, reserva provisoriamente e persiste PREPARE no WAL
func (p *Participante2PC) ProcessarPrepare(req modelos.PrepareRequest) (modelos.PrepareResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, existe := p.transacoes[req.TxID]; existe {
		return modelos.PrepareResponse{
			TxID:    req.TxID,
			VotoSim: false,
			Motivo:  "transacao ja existente no participante",
		}, ErrTransacaoJaExiste
	}

	recursoID := fmt.Sprintf("carona_%d_%s_%s", req.CaronaID, req.Origem, req.Destino)

	err := p.servico.BloquearAssentoProvisorio(req.CaronaID, req.Origem, req.Destino, req.Passageiro)
	if err != nil {
		return modelos.PrepareResponse{
			TxID:    req.TxID,
			VotoSim: false,
			Motivo:  fmt.Sprintf("assento indisponivel: %v", err),
		}, nil
	}

	if p.wal != nil {
		detalhes := fmt.Sprintf("passageiro=%s", req.Passageiro)
		if errWal := p.wal.Gravar(req.TxID, LogPrepare, recursoID, detalhes); errWal != nil {
			// Em caso de falha de I/O em disco, desfaz o bloqueio em memoria e vota NAO
			_ = p.servico.LiberarAssentoProvisorio(req.CaronaID, req.Origem, req.Destino, req.Passageiro)
			return modelos.PrepareResponse{
				TxID:    req.TxID,
				VotoSim: false,
				Motivo:  "erro ao sincronizar log de transacao no disco",
			}, errWal
		}
	}

	p.transacoes[req.TxID] = &TransacaoLocal{
		TxID:       req.TxID,
		Estado:     modelos.TxPreparada,
		CaronaID:   req.CaronaID,
		Origem:     req.Origem,
		Destino:    req.Destino,
		Passageiro: req.Passageiro,
		RecursoID:  recursoID,
		Timestamp:  time.Now(),
	}

	return modelos.PrepareResponse{
		TxID:    req.TxID,
		VotoSim: true,
	}, nil
}

// ProcessarCommit efetiva a reserva definitivamente e persiste COMMIT no WAL
func (p *Participante2PC) ProcessarCommit(txID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, existe := p.transacoes[txID]
	if !existe {
		return ErrTransacaoNaoEncontrada
	}

	if tx.Estado != modelos.TxPreparada {
		return fmt.Errorf("%w: esperado PREPARADA, atual %s", ErrEstadoInvalido, tx.Estado)
	}

	if p.wal != nil {
		_ = p.wal.Gravar(txID, LogCommit, tx.RecursoID, "commit_efetivado")
	}

	_ = p.servico.ConfirmarAssentoDefinitivo(tx.CaronaID, tx.Origem, tx.Destino, tx.Passageiro)

	tx.Estado = modelos.TxConfirmada
	delete(p.transacoes, txID)

	return nil
}

// ProcessarAbort desfaz o bloqueio temporario e persiste ABORT no WAL
func (p *Participante2PC) ProcessarAbort(txID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, existe := p.transacoes[txID]
	if !existe {
		// Se o no nao possui a transacao, grava abort
		if p.wal != nil {
			_ = p.wal.Gravar(txID, LogAbort, "", "abort_transacao_desconhecida")
		}
		return nil
	}

	if p.wal != nil {
		_ = p.wal.Gravar(txID, LogAbort, tx.RecursoID, "abort_solicitado")
	}

	_ = p.servico.LiberarAssentoProvisorio(tx.CaronaID, tx.Origem, tx.Destino, tx.Passageiro)

	tx.Estado = modelos.TxAbortada
	delete(p.transacoes, txID)

	return nil
}

// RecuperarAposFalha reconstitui o estado de transacoes ativas lendo o WAL apos o no religar
func (p *Participante2PC) RecuperarAposFalha() error {
	if p.wal == nil {
		return nil
	}

	historico, err := p.wal.RecuperarEstado()
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for txID, ultimoEstado := range historico {
		switch ultimoEstado {
		case LogPrepare:
			// mantem em estado preparado
			p.transacoes[txID] = &TransacaoLocal{
				TxID:      txID,
				Estado:    modelos.TxPreparada,
				Timestamp: time.Now(),
			}
		case LogCommit, LogAbort:
			delete(p.transacoes, txID)
		}
	}

	return nil
}

// LimparTransacoesExpiradas liberta assentos de preparacoes orfas cujo coordenador desapareceu
func (p *Participante2PC) LimparTransacoesExpiradas(timeout time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	agora := time.Now()
	for txID, tx := range p.transacoes {
		if tx.Estado == modelos.TxPreparada && agora.Sub(tx.Timestamp) > timeout {
			if p.wal != nil {
				_ = p.wal.Gravar(txID, LogAbort, tx.RecursoID, "abort_por_timeout_expirado")
			}
			_ = p.servico.LiberarAssentoProvisorio(tx.CaronaID, tx.Origem, tx.Destino, tx.Passageiro)
			delete(p.transacoes, txID)
		}
	}
}
