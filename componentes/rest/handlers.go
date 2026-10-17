package rest

import (
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/distribuido"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type ServicoCaronas interface {
	BuscarTrechos(origem, destino string) []modelos.TrechoViagem
	BloquearAssentoProvisorio(caronaID int, origem, destino, passageiro string) error
	LiberarAssentoProvisorio(caronaID int, origem, destino, passageiro string) error
	ConfirmarAssentoDefinitivo(caronaID int, origem, destino, passageiro string) error
}

type HandlersREST struct {
	idEmpresa string
	servico   ServicoCaronas
	wal       *distribuido.LogTransacoes
	mu        sync.Mutex

	transacoesAtivas map[string]modelos.PrepareRequest
}

func NovoHandlersREST(idEmpresa string, servico ServicoCaronas, wal *distribuido.LogTransacoes) *HandlersREST {
	return &HandlersREST{
		idEmpresa:        idEmpresa,
		servico:          servico,
		wal:              wal,
		transacoesAtivas: make(map[string]modelos.PrepareRequest),
	}
}

func (h *HandlersREST) RegistrarRotas(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/trechos", h.HandleBuscarTrechos)
	mux.HandleFunc("/api/v1/transacoes/prepare", h.HandlePrepare)
	mux.HandleFunc("/api/v1/transacoes/commit", h.HandleCommit)
	mux.HandleFunc("/api/v1/transacoes/abort", h.HandleAbort)
}

// GET /api/v1/trechos?origem=X&destino=Y
func (h *HandlersREST) HandleBuscarTrechos(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}

	origem := r.URL.Query().Get("origem")
	destino := r.URL.Query().Get("destino")

	trechos := h.servico.BuscarTrechos(origem, destino)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(trechos)
}

// POST /api/v1/transacoes/prepare
func (h *HandlersREST) HandlePrepare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}

	var req modelos.PrepareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responderJSON(w, http.StatusBadRequest, modelos.PrepareResponse{
			TxID:    req.TxID,
			VotoSim: false,
			Motivo:  "corpo da requisicao malformado",
		})
		return
	}

	recursoID := fmt.Sprintf("carona_%d_%s_%s", req.CaronaID, req.Origem, req.Destino)

	h.mu.Lock()
	defer h.mu.Unlock()

	err := h.servico.BloquearAssentoProvisorio(req.CaronaID, req.Origem, req.Destino, req.Passageiro)
	if err != nil {
		responderJSON(w, http.StatusOK, modelos.PrepareResponse{
			TxID:    req.TxID,
			VotoSim: false,
			Motivo:  fmt.Sprintf("indisponibilidade de assento: %v", err),
		})
		return
	}

	if h.wal != nil {
		errWal := h.wal.Gravar(req.TxID, distribuido.LogPrepare, recursoID, req.Passageiro)
		if errWal != nil {
			_ = h.servico.LiberarAssentoProvisorio(req.CaronaID, req.Origem, req.Destino, req.Passageiro)
			responderJSON(w, http.StatusInternalServerError, modelos.PrepareResponse{
				TxID:    req.TxID,
				VotoSim: false,
				Motivo:  "falha ao persistir voto em disco seguro",
			})
			return
		}
	}

	h.transacoesAtivas[req.TxID] = req

	responderJSON(w, http.StatusOK, modelos.PrepareResponse{
		TxID:    req.TxID,
		VotoSim: true,
	})
}

// POST /api/v1/transacoes/commit
func (h *HandlersREST) HandleCommit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}

	var req modelos.DecisaoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "json invalido", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	dadosPrep, existe := h.transacoesAtivas[req.TxID]
	h.mu.Unlock()

	recursoID := ""
	if existe {
		recursoID = fmt.Sprintf("carona_%d_%s_%s", dadosPrep.CaronaID, dadosPrep.Origem, dadosPrep.Destino)
	}

	if h.wal != nil {
		_ = h.wal.Gravar(req.TxID, distribuido.LogCommit, recursoID, "reserva_confirmada")
	}

	if existe {
		_ = h.servico.ConfirmarAssentoDefinitivo(dadosPrep.CaronaID, dadosPrep.Origem, dadosPrep.Destino, dadosPrep.Passageiro)
		h.mu.Lock()
		delete(h.transacoesAtivas, req.TxID)
		h.mu.Unlock()
	}

	responderJSON(w, http.StatusOK, map[string]any{
		"tx_id":    req.TxID,
		"status":   "COMMITTED",
		"sucesso":  true,
		"mensagem": "transacao confirmada com sucesso",
	})
}

// POST /api/v1/transacoes/abort
func (h *HandlersREST) HandleAbort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "metodo nao permitido", http.StatusMethodNotAllowed)
		return
	}

	var req modelos.DecisaoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "json invalido", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	dadosPrep, existe := h.transacoesAtivas[req.TxID]
	h.mu.Unlock()

	recursoID := ""
	if existe {
		recursoID = fmt.Sprintf("carona_%d_%s_%s", dadosPrep.CaronaID, dadosPrep.Origem, dadosPrep.Destino)
		_ = h.servico.LiberarAssentoProvisorio(dadosPrep.CaronaID, dadosPrep.Origem, dadosPrep.Destino, dadosPrep.Passageiro)
		h.mu.Lock()
		delete(h.transacoesAtivas, req.TxID)
		h.mu.Unlock()
	}

	if h.wal != nil {
		_ = h.wal.Gravar(req.TxID, distribuido.LogAbort, recursoID, "reserva_abortada")
	}

	responderJSON(w, http.StatusOK, map[string]any{
		"tx_id":    req.TxID,
		"status":   "ABORTED",
		"sucesso":  true,
		"mensagem": "transacao abortada e vaga liberada",
	})
}

func responderJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
