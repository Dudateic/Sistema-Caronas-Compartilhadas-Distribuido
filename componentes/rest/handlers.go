package rest

import (
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/distribuido"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
	"encoding/json"
	"errors"
	"net/http"
)

type ServicoCaronas interface {
	BuscarTrechos(origem, destino string) []modelos.TrechoViagem
	BloquearAssentoProvisorio(caronaID int, origem, destino, passageiro string) error
	LiberarAssentoProvisorio(caronaID int, origem, destino, passageiro string) error
	ConfirmarAssentoDefinitivo(caronaID int, origem, destino, passageiro string) error
	LiberarAssentoConfirmado(caronaID int, origem, destino, passageiro string) error
}

// ParticipanteTransacional é o papel de participante do 2PC nesta empresa.
type ParticipanteTransacional interface {
	ProcessarPrepare(req modelos.PrepareRequest) (modelos.PrepareResponse, error)
	ProcessarCommit(txID string) error
	ProcessarAbort(txID string) error
	ConsultarEstado(txID string) string
}

// DecisorTransacional é o papel de coordenador: informa o destino de uma transação.
type DecisorTransacional interface {
	Decisao(txID string) string
}

type VerificadorCancelamentos interface {
	VerificarTrechosCancelados(empresaAlvo string, ativos []modelos.TrechoOcupadoInfo) []modelos.TrechoOcupadoInfo
}

type HandlersREST struct {
	idEmpresa        string
	servico          ServicoCaronas
	participante     ParticipanteTransacional
	decisor          DecisorTransacional
	verifReservas    VerificadorCancelamentos
	aoCancelarRemoto func(empresa string, caronaID int)
}

func NovoHandlersREST(
	idEmpresa string,
	servico ServicoCaronas,
	participante ParticipanteTransacional,
	decisor DecisorTransacional,
	verif VerificadorCancelamentos,
	aoCancelarRemoto func(empresa string, caronaID int),
) *HandlersREST {
	return &HandlersREST{
		idEmpresa:        idEmpresa,
		servico:          servico,
		participante:     participante,
		decisor:          decisor,
		verifReservas:    verif,
		aoCancelarRemoto: aoCancelarRemoto,
	}
}

func (h *HandlersREST) RegistrarRotas(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/trechos", h.HandleBuscarTrechos)
	mux.HandleFunc("/api/v1/transacoes/prepare", h.HandlePrepare)
	mux.HandleFunc("/api/v1/transacoes/commit", h.HandleCommit)
	mux.HandleFunc("/api/v1/transacoes/abort", h.HandleAbort)
	mux.HandleFunc("/api/v1/transacoes/decisao", h.HandleDecisao)
	mux.HandleFunc("/api/v1/transacoes/estado", h.HandleEstado)
	mux.HandleFunc("/api/v1/trechos/cancelar", h.HandleCancelarTrechoRemoto)
	mux.HandleFunc("/api/v1/sync/status", h.HandleSyncStatus)
	mux.HandleFunc("/api/v1/caronas/cancelada", h.HandleNotificarCaronaCancelada)
}

// GET /api/v1/trechos?origem=&destino=
// 200 com a lista de trechos com assento livre (pode ser vazia).
func (h *HandlersREST) HandleBuscarTrechos(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}

	trechos := h.servico.BuscarTrechos(r.URL.Query().Get("origem"), r.URL.Query().Get("destino"))
	if trechos == nil {
		trechos = []modelos.TrechoViagem{}
	}
	responderJSON(w, http.StatusOK, trechos)
}

// POST /api/v1/transacoes/prepare
// 200 voto SIM (idempotente) | 409 voto NAO | 400 corpo invalido | 500 falha ao gravar o log
func (h *HandlersREST) HandlePrepare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}

	var req modelos.PrepareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TxID == "" {
		responderJSON(w, http.StatusBadRequest, modelos.PrepareResponse{
			TxID: req.TxID, VotoSim: false, Motivo: "corpo da requisicao malformado",
		})
		return
	}

	resp, err := h.participante.ProcessarPrepare(req)
	switch {
	case err != nil:
		responderJSON(w, http.StatusInternalServerError, resp)
	case resp.VotoSim:
		responderJSON(w, http.StatusOK, resp)
	default:
		responderJSON(w, http.StatusConflict, resp)
	}
}

// POST /api/v1/transacoes/commit
// 200 confirmado (idempotente) | 404 transacao desconhecida | 409 ja abortada | 500 falha de log
func (h *HandlersREST) HandleCommit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}

	var req modelos.DecisaoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TxID == "" {
		http.Error(w, "json invalido", http.StatusBadRequest)
		return
	}

	err := h.participante.ProcessarCommit(req.TxID)
	h.responderDecisao(w, req.TxID, "COMMITTED", err)
}

// POST /api/v1/transacoes/abort
// 200 abortado (idempotente; inclusive se a transacao era desconhecida) | 409 ja confirmada | 500 falha de log
func (h *HandlersREST) HandleAbort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}

	var req modelos.DecisaoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TxID == "" {
		http.Error(w, "json invalido", http.StatusBadRequest)
		return
	}

	err := h.participante.ProcessarAbort(req.TxID)
	h.responderDecisao(w, req.TxID, "ABORTED", err)
}

func (h *HandlersREST) responderDecisao(w http.ResponseWriter, txID, status string, err error) {
	switch {
	case err == nil:
		responderJSON(w, http.StatusOK, map[string]any{"tx_id": txID, "status": status, "sucesso": true})
	case errors.Is(err, distribuido.ErrTransacaoNaoEncontrada):
		responderJSON(w, http.StatusNotFound, map[string]any{"tx_id": txID, "sucesso": false, "erro": err.Error()})
	case errors.Is(err, distribuido.ErrEstadoInvalido):
		responderJSON(w, http.StatusConflict, map[string]any{"tx_id": txID, "sucesso": false, "erro": err.Error()})
	default:
		responderJSON(w, http.StatusInternalServerError, map[string]any{"tx_id": txID, "sucesso": false, "erro": err.Error()})
	}
}

// GET /api/v1/transacoes/decisao?tx_id=X
// Usado por um participante que votou SIM e ficou sem resposta do coordenador.
// 200 {decisao: COMMIT | ABORT | EM_ANDAMENTO} | 400 sem tx_id
func (h *HandlersREST) HandleDecisao(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}
	txID := r.URL.Query().Get("tx_id")
	if txID == "" {
		http.Error(w, "tx_id obrigatorio", http.StatusBadRequest)
		return
	}
	responderJSON(w, http.StatusOK, modelos.DecisaoResponse{TxID: txID, Decisao: h.decisor.Decisao(txID)})
}

// GET /api/v1/transacoes/estado?tx_id=X
// Usado por outro PARTICIPANTE quando o coordenador está fora do ar (terminação cooperativa).
// 200 {estado: PREPARADA | CONFIRMADA | ABORTADA | DESCONHECIDA} | 400 sem tx_id
// Atenção: perguntar sobre uma transação que este participante nunca viu a registra como
// ABORTADA (promessa de não votar SIM depois), o que é seguro para quem perguntou.
func (h *HandlersREST) HandleEstado(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}
	txID := r.URL.Query().Get("tx_id")
	if txID == "" {
		http.Error(w, "tx_id obrigatorio", http.StatusBadRequest)
		return
	}
	responderJSON(w, http.StatusOK, modelos.EstadoResponse{TxID: txID, Estado: h.participante.ConsultarEstado(txID)})
}

// POST /api/v1/trechos/cancelar
func (h *HandlersREST) HandleCancelarTrechoRemoto(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CaronaID   int    `json:"carona_id"`
		Origem     string `json:"origem"`
		Destino    string `json:"destino"`
		Passageiro string `json:"passageiro"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "payload invalido", http.StatusBadRequest)
		return
	}

	err := h.servico.LiberarAssentoConfirmado(req.CaronaID, req.Origem, req.Destino, req.Passageiro)
	if err != nil {
		responderJSON(w, http.StatusNotFound, map[string]any{"sucesso": false, "erro": err.Error()})
		return
	}

	responderJSON(w, http.StatusOK, map[string]any{"sucesso": true})
}

// POST /api/v1/sync/status
func (h *HandlersREST) HandleSyncStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}

	var req modelos.SyncStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "payload malformado", http.StatusBadRequest)
		return
	}

	var paraLiberar []modelos.TrechoOcupadoInfo
	if h.verifReservas != nil {
		paraLiberar = h.verifReservas.VerificarTrechosCancelados(req.EmpresaSolitante, req.TrechosAtivos)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(modelos.SyncStatusResponse{
		TrechosParaLiberar: paraLiberar,
	})
}

// POST /api/v1/caronas/cancelada
func (h *HandlersREST) HandleNotificarCaronaCancelada(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Empresa  string `json:"empresa"`
		CaronaID int    `json:"carona_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "payload invalido", http.StatusBadRequest)
		return
	}

	if h.aoCancelarRemoto != nil {
		h.aoCancelarRemoto(req.Empresa, req.CaronaID)
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"sucesso": true})
}

func responderJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
