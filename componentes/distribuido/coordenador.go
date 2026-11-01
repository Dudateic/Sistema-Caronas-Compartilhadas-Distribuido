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
	ErrVotoNaoRecebido = errors.New("um ou mais participantes recusaram o voto prepare")
	ErrTimeoutFase1    = errors.New("tempo esgotado ao aguardar votos da fase prepare")
)

// IntervaloReenvioFase2 é o intervalo entre tentativas de entregar COMMIT/ABORT a um participante que não respondeu.
var IntervaloReenvioFase2 = 2 * time.Second

// MaxTentativasFase2 limita os reenvios em segundo plano (o participante ainda pode
// resolver sozinho consultando a decisão em GET /api/v1/transacoes/decisao).
const MaxTentativasFase2 = 150

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
	relogio            *RelogioLamport
	mu                 sync.RWMutex

	emAndamento map[string]bool   // transações na fase 1
	decisoes    map[string]string // TxID -> COMMIT | ABORT
}

func NovoCoordenador2PC(
	idEmpresaLocal string,
	enderecoLocal string,
	clienteHTTP ClienteConsorcio,
	wal *LogTransacoes,
	participanteLocal *Participante2PC,
	enderecos MapaEmpresas,
) *Coordenador2PC {
	relogio := &RelogioLamport{}
	if participanteLocal != nil {
		relogio = participanteLocal.Relogio() // um único relógio por empresa
	}
	return &Coordenador2PC{
		idEmpresaLocal:     idEmpresaLocal,
		enderecoLocal:      enderecoLocal,
		clienteHTTP:        clienteHTTP,
		wal:                wal,
		participanteLocal:  participanteLocal,
		tabelaEnderecos:    enderecos,
		timeoutFasePrepare: 3 * time.Second,
		relogio:            relogio,
		emAndamento:        make(map[string]bool),
		decisoes:           make(map[string]string),
	}
}

// DefinirTimeoutPrepare ajusta o tempo máximo de espera pelos votos da fase 1.
func (c *Coordenador2PC) DefinirTimeoutPrepare(d time.Duration) { c.timeoutFasePrepare = d }

type resultadoVoto struct {
	empresaID string
	endereco  string
	sucesso   bool
	motivo    string
	err       error
}

type inicioTx struct {
	Participantes map[string]string `json:"participantes"`
	Passageiro    string            `json:"passageiro"`
}

func (c *Coordenador2PC) gravar(txID string, tipo TipoRegistroLog, detalhes string) error {
	if c.wal == nil {
		return nil
	}
	return c.wal.Gravar(txID, tipo, "", detalhes)
}

// Decisao informa o destino de uma transação aos participantes que perguntam.
// Sem registro algum, vale a regra do "abort presumido": se o coordenador não
// decidiu COMMIT, a transação não pode ter sido confirmada.
func (c *Coordenador2PC) Decisao(txID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.emAndamento[txID] {
		return modelos.DecisaoEmAndamento
	}
	if d, ok := c.decisoes[txID]; ok {
		return d
	}
	return modelos.DecisaoAbort
}

func (c *Coordenador2PC) enderecoDe(empresa string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for nomePar, urlPar := range c.tabelaEnderecos {
		if strings.EqualFold(nomePar, empresa) {
			return urlPar, true
		}
	}
	return "", false
}

func (c *Coordenador2PC) ehLocal(empresa string) bool {
	e := strings.TrimSpace(empresa)
	return e == "" || strings.EqualFold(e, c.idEmpresaLocal)
}

func (c *Coordenador2PC) ExecutarReservaAtomica(txID string, passageiro string, itinerario modelos.Itinerario, _ int64) (bool, string, error) {
	if len(itinerario.Trechos) == 0 {
		return false, "itinerario vazio", errors.New("nenhum trecho fornecido")
	}

	timestampLamport := c.relogio.Tick()

	// Pré-mapeia todos os participantes para que todos recebam a decisão, mesmo em timeout
	participantes := make(map[string]string) // empresa -> endereço ("" = local)
	for _, t := range itinerario.Trechos {
		if c.ehLocal(t.Empresa) {
			participantes[c.idEmpresaLocal] = ""
			continue
		}
		emp := strings.ToUpper(strings.TrimSpace(t.Empresa))
		if url, ok := c.enderecoDe(emp); ok {
			participantes[emp] = url
		}
	}

	// Registra o início (com a lista de participantes) ANTES de qualquer PREPARE:
	// se o coordenador cair, a recuperação sabe a quem enviar o ABORT.
	detInicio, _ := json.Marshal(inicioTx{Participantes: participantes, Passageiro: passageiro})
	if err := c.gravar(txID, LogInicio, string(detInicio)); err != nil {
		return false, "falha ao gravar o log do coordenador", err
	}
	c.mu.Lock()
	c.emAndamento[txID] = true
	c.mu.Unlock()

	// Fase 1: PREPARE em paralelo
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
			if !v.sucesso {
				todosVotaramSim = false
				if motivoFalha == "" {
					motivoFalha = fmt.Sprintf("[%s]: %s", v.empresaID, v.motivo)
					if v.err != nil && v.motivo == "" {
						motivoFalha = fmt.Sprintf("[%s]: %v", v.empresaID, v.err)
					}
				}
			}
		case <-limiteTempo:
			todosVotaramSim = false
			motivoFalha = "timeout na fase de preparacao de um ou mais participantes"
			break loopVotos
		}
	}

	confirmar := todosVotaramSim && respostasColetadas == len(itinerario.Trechos)

	// Decisão global: gravada em disco ANTES de ser comunicada (ponto de commit da transação)
	decisao := modelos.DecisaoAbort
	if confirmar {
		if err := c.gravar(txID, LogCommit, "global_commit_aprovado"); err != nil {
			confirmar = false
			motivoFalha = "falha ao gravar a decisao global"
		} else {
			decisao = modelos.DecisaoCommit
		}
	}
	if !confirmar {
		_ = c.gravar(txID, LogAbort, fmt.Sprintf("global_abort_motivo=%s", motivoFalha))
	}

	c.mu.Lock()
	c.decisoes[txID] = decisao
	delete(c.emAndamento, txID)
	c.mu.Unlock()

	// Fase 2: COMMIT ou ABORT; quem não respondeu é re-tentado em segundo plano
	c.concluirFase2(txID, participantes, confirmar)

	if confirmar {
		return true, "reserva confirmada com sucesso em todo o consorcio", nil
	}
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
		TxID:        txID,
		CaronaID:    trecho.CaronaID,
		Origem:      trecho.Origem,
		Destino:     trecho.Destino,
		Passageiro:  passageiro,
		Timestamp:   timestampLamport,
		Coordenador: c.idEmpresaLocal,
	}

	if c.ehLocal(trecho.Empresa) {
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

	enderecoBase, existe := c.enderecoDe(trecho.Empresa)
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
	if err == nil && resp.Timestamp > 0 {
		c.relogio.Atualizar(resp.Timestamp)
	}
	canal <- resultadoVoto{
		empresaID: trecho.Empresa,
		endereco:  enderecoBase,
		sucesso:   err == nil && resp.VotoSim,
		motivo:    resp.Motivo,
		err:       err,
	}
}

// entregarDecisao tenta uma vez entregar a decisão a um participante.
func (c *Coordenador2PC) entregarDecisao(txID, empresa, endereco string, confirmar bool) error {
	if strings.EqualFold(empresa, c.idEmpresaLocal) {
		if confirmar {
			return c.participanteLocal.ProcessarCommit(txID)
		}
		return c.participanteLocal.ProcessarAbort(txID)
	}
	if confirmar {
		return c.clienteHTTP.EnviarCommit(endereco, txID)
	}
	return c.clienteHTTP.EnviarAbort(endereco, txID)
}

// concluirFase2 envia a decisão a todos em paralelo. Os que falharem entram numa
// rotina de reenvio; quando todos confirmam, grava FIM no log do coordenador.
func (c *Coordenador2PC) concluirFase2(txID string, participantes map[string]string, confirmar bool) {
	var mu sync.Mutex
	falhos := map[string]string{}
	var wg sync.WaitGroup

	for empID, end := range participantes {
		wg.Add(1)
		go func(id, endereco string) {
			defer wg.Done()
			if err := c.entregarDecisao(txID, id, endereco, confirmar); err != nil {
				mu.Lock()
				falhos[id] = endereco
				mu.Unlock()
			}
		}(empID, end)
	}
	wg.Wait()

	if len(falhos) == 0 {
		_ = c.gravar(txID, LogFim, "decisao_entregue")
		return
	}
	go c.reenviarFase2(txID, falhos, confirmar)
}

func (c *Coordenador2PC) reenviarFase2(txID string, pendentes map[string]string, confirmar bool) {
	for tentativa := 0; tentativa < MaxTentativasFase2 && len(pendentes) > 0; tentativa++ {
		time.Sleep(IntervaloReenvioFase2)
		for id, end := range pendentes {
			if err := c.entregarDecisao(txID, id, end, confirmar); err == nil {
				delete(pendentes, id)
			}
		}
	}
	if len(pendentes) == 0 {
		_ = c.gravar(txID, LogFim, "decisao_entregue_apos_reenvio")
	}
}

// Recuperar relê o log do coordenador após uma queda:
//   - INICIO sem decisão -> ABORT presumido, enviado a todos os participantes
//   - COMMIT/ABORT sem FIM -> reenvia a decisão
func (c *Coordenador2PC) Recuperar() error {
	if c.wal == nil {
		return nil
	}
	entradas, err := c.wal.Entradas()
	if err != nil {
		return err
	}

	type estado struct {
		inicio  *inicioTx
		decisao string
		fim     bool
	}
	porTx := map[string]*estado{}
	var ordem []string
	for _, e := range entradas {
		st, ok := porTx[e.TxID]
		if !ok {
			st = &estado{}
			porTx[e.TxID] = st
			ordem = append(ordem, e.TxID)
		}
		switch e.Tipo {
		case LogInicio:
			var in inicioTx
			if json.Unmarshal([]byte(e.Detalhes), &in) == nil {
				st.inicio = &in
			}
		case LogCommit:
			st.decisao = modelos.DecisaoCommit
		case LogAbort:
			st.decisao = modelos.DecisaoAbort
		case LogFim:
			st.fim = true
		}
	}

	for _, txID := range ordem {
		st := porTx[txID]
		if st.decisao == "" {
			if st.inicio == nil {
				continue
			}
			st.decisao = modelos.DecisaoAbort
			_ = c.gravar(txID, LogAbort, "abort_presumido_na_recuperacao")
		}
		c.mu.Lock()
		c.decisoes[txID] = st.decisao
		c.mu.Unlock()

		if st.fim || st.inicio == nil {
			continue
		}
		go c.concluirFase2(txID, st.inicio.Participantes, st.decisao == modelos.DecisaoCommit)
	}
	return nil
}
