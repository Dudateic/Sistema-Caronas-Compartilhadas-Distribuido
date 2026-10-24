package distribuido

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

var (
	ErrVotoNaoRecebido = errors.New("um ou mais participantes recusaram o voto prepare")
	ErrTimeoutFase1    = errors.New("tempo esgotado ao aguardar votos da fase prepare")
)

type ClienteConsorcio interface {
	EnviarPrepare(enderecoBase string, prepareReq modelos.PrepareRequest) (modelos.PrepareResponse, error)
	EnviarCommit(enderecoBase, txID string) error
	EnviarAbort(enderecoBase, txID string) error
}

type MapaEmpresas map[string]string

type Coordenador2PC struct {
	idEmpresaLocal     string
	enderecoLocal      string
	clienteHTTP        ClienteConsorcio
	wal                *LogTransacoes
	participanteLocal  *Participante2PC
	tabelaEnderecos    MapaEmpresas
	timeoutFasePrepare time.Duration
	mu                 sync.RWMutex
}

func NovoCoordenador2PC(
	idEmpresaLocal string,
	enderecoLocal string,
	clienteHTTP ClienteConsorcio,
	wal *LogTransacoes,
	participanteLocal *Participante2PC,
	enderecos MapaEmpresas,
) *Coordenador2PC {
	return &Coordenador2PC{
		idEmpresaLocal:     idEmpresaLocal,
		enderecoLocal:      enderecoLocal,
		clienteHTTP:        clienteHTTP,
		wal:                wal,
		participanteLocal:  participanteLocal,
		tabelaEnderecos:    enderecos,
		timeoutFasePrepare: 3 * time.Second,
	}
}

type resultadoVoto struct {
	empresaID string
	endereco  string
	sucesso   bool
	motivo    string
	err       error
}

func (c *Coordenador2PC) ExecutarReservaAtomica(txID string, passageiro string, itinerario modelos.Itinerario, timestampLamport int64) (bool, string, error) {
	if len(itinerario.Trechos) == 0 {
		return false, "itinerario vazio", errors.New("nenhum trecho fornecido")
	}

	recursoDesc := fmt.Sprintf("itinerario_%d_trechos", len(itinerario.Trechos))

	if c.wal != nil {
		_ = c.wal.Gravar(txID, LogPrepare, recursoDesc, fmt.Sprintf("iniciando_coordenacao_passageiro=%s", passageiro))
	}

	// Pré-mapeia todos os participantes envolvidos no itinerário
	// para garantir que mesmo em caso de timeout todos recebam ABORT
	enderecosParticipantes := make(map[string]string) // empresaID -> enderecoBase
	for _, t := range itinerario.Trechos {
		emp := strings.ToUpper(strings.TrimSpace(t.Empresa))
		if emp == "" || strings.EqualFold(emp, c.idEmpresaLocal) {
			enderecosParticipantes[c.idEmpresaLocal] = c.enderecoLocal
			continue
		}

		c.mu.RLock()
		for nomePar, urlPar := range c.tabelaEnderecos {
			if strings.EqualFold(nomePar, emp) {
				enderecosParticipantes[emp] = urlPar
				break
			}
		}
		c.mu.RUnlock()
	}

	// Fase 1: Coleta de Votos (PREPARE)
	canalVotos := make(chan resultadoVoto, len(itinerario.Trechos))
	var wg sync.WaitGroup

	for _, trecho := range itinerario.Trechos {
		wg.Add(1)
		go func(t modelos.TrechoViagem) {
			defer wg.Done()
			c.coletarVotoPrepare(txID, passageiro, t, timestampLamport, canalVotos)
		}(trecho)
	}

	go func() {
		wg.Wait()
		close(canalVotos)
	}()

	todosVotaramSim := true
	var motivoFalha string

	limiteTempo := time.After(c.timeoutFasePrepare)
	respostasColetadas := 0

loopVotos:
	for respostasColetadas < len(itinerario.Trechos) {
		select {
		case v, ok := <-canalVotos:
			if !ok {
				break loopVotos
			}
			respostasColetadas++

			if v.endereco != "" {
				enderecosParticipantes[v.empresaID] = v.endereco
			}

			if !v.sucesso {
				todosVotaramSim = false
				if motivoFalha == "" {
					motivoFalha = fmt.Sprintf("[%s]: %s", v.empresaID, v.motivo)
				}
			}

		case <-limiteTempo:
			todosVotaramSim = false
			motivoFalha = "timeout na fase de preparacao de um ou mais participantes"
			break loopVotos
		}
	}

	// Fase 2: Decisão Global (COMMIT ou ABORT)
	if todosVotaramSim && respostasColetadas == len(itinerario.Trechos) {
		if c.wal != nil {
			_ = c.wal.Gravar(txID, LogCommit, recursoDesc, "global_commit_aprovado")
		}

		c.dispararFase2(txID, enderecosParticipantes, true)
		return true, "reserva confirmada com sucesso em todo o consorcio", nil
	}

	if c.wal != nil {
		_ = c.wal.Gravar(txID, LogAbort, recursoDesc, fmt.Sprintf("global_abort_motivo=%s", motivoFalha))
	}

	// Dispara ABORT para todos os participantes que foram mapeados
	c.dispararFase2(txID, enderecosParticipantes, false)
	return false, motivoFalha, ErrVotoNaoRecebido
}

// coletarVotoPrepare envia o PREPARE via memória se local, ou via HTTP se remoto
func (c *Coordenador2PC) coletarVotoPrepare(
	txID string,
	passageiro string,
	trecho modelos.TrechoViagem,
	timestampLamport int64,
	canal chan<- resultadoVoto,
) {
	req := modelos.PrepareRequest{
		TxID:       txID,
		CaronaID:   trecho.CaronaID,
		Origem:     trecho.Origem,
		Destino:    trecho.Destino,
		Passageiro: passageiro,
		Timestamp:  timestampLamport,
	}

	empresaTrecho := strings.ToUpper(strings.TrimSpace(trecho.Empresa))

	// Caso o trecho pertença à própria empresa coordenadora: atalho local em memória
	if empresaTrecho == "" || strings.EqualFold(empresaTrecho, c.idEmpresaLocal) {
		resp, err := c.participanteLocal.ProcessarPrepare(req)
		canal <- resultadoVoto{
			empresaID: c.idEmpresaLocal,
			endereco:  c.enderecoLocal,
			sucesso:   err == nil && resp.VotoSim,
			motivo:    resp.Motivo,
			err:       err,
		}
		return
	}

	// Caso pertença a outra empresa do consórcio: busca URL no mapa de parceiros
	c.mu.RLock()
	var enderecoBase string
	var existe bool
	for nomePar, urlPar := range c.tabelaEnderecos {
		if strings.EqualFold(nomePar, empresaTrecho) {
			enderecoBase = urlPar
			existe = true
			break
		}
	}
	c.mu.RUnlock()

	if !existe {
		canal <- resultadoVoto{
			empresaID: trecho.Empresa,
			sucesso:   false,
			motivo:    fmt.Sprintf("endereco do no %s nao configurado", trecho.Empresa),
			err:       errors.New("empresa desconhecida"),
		}
		return
	}

	resp, err := c.clienteHTTP.EnviarPrepare(enderecoBase, req)
	canal <- resultadoVoto{
		empresaID: trecho.Empresa,
		endereco:  enderecoBase,
		sucesso:   err == nil && resp.VotoSim,
		motivo:    resp.Motivo,
		err:       err,
	}
}

// dispararFase2 emite o COMMIT ou ABORT em paralelo para todas as cohortes contactadas
func (c *Coordenador2PC) dispararFase2(txID string, participantes map[string]string, confirmar bool) {
	var wg sync.WaitGroup

	for empID, endereco := range participantes {
		wg.Add(1)
		go func(id, end string) {
			defer wg.Done()

			if strings.EqualFold(id, c.idEmpresaLocal) {
				if confirmar {
					_ = c.participanteLocal.ProcessarCommit(txID)
				} else {
					_ = c.participanteLocal.ProcessarAbort(txID)
				}
				return
			}

			if confirmar {
				_ = c.clienteHTTP.EnviarCommit(end, txID)
			} else {
				_ = c.clienteHTTP.EnviarAbort(end, txID)
			}
		}(empID, endereco)
	}

	wg.Wait()
}
