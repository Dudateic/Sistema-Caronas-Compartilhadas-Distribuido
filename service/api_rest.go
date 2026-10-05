package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
)

//	POST /api/v1/transacoes/prepare            -> 200 (voto SIM) | 409 (voto NAO) | 400
//	POST /api/v1/transacoes/{tx_id}/commit     -> 200 | 404 (desconhecida) | 409 (nao pode confirmar) | 400
//	POST /api/v1/transacoes/{tx_id}/rollback   -> 200 | 409 (ja confirmada) | 400

func NovoHandlerAPI() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/transacoes/prepare", handlerPrepare)
	mux.HandleFunc("POST /api/v1/transacoes/{tx_id}/commit", handlerCommit)
	mux.HandleFunc("POST /api/v1/transacoes/{tx_id}/rollback", handlerRollback)
	return mux
}

func escreverJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func erroHTTP(w http.ResponseWriter, status int, msg string) {
	escreverJSON(w, status, protocolo.ErroHTTP{Status: status, Mensagem: msg})
}

func decodificar(w http.ResponseWriter, r *http.Request, destino any, opcional bool) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	err := json.NewDecoder(r.Body).Decode(destino)
	if err == nil || (opcional && errors.Is(err, io.EOF)) {
		return true
	}
	erroHTTP(w, http.StatusBadRequest, "corpo JSON invalido")
	return false
}

func handlerPrepare(w http.ResponseWriter, r *http.Request) {
	var p protocolo.PedidoBloqueio
	if !decodificar(w, r, &p, false) {
		return
	}
	if p.TxID == "" || p.CaronaID <= 0 || p.Origem == "" || p.Destino == "" || p.Passageiro == "" {
		erroHTTP(w, http.StatusBadRequest, "campos obrigatorios: tx_id, carona_id, origem, destino, passageiro")
		return
	}
	status, resp := Preparar(p)
	escreverJSON(w, status, resp)
}

func handlerCommit(w http.ResponseWriter, r *http.Request) {
	txID := r.PathValue("tx_id")
	var corpo protocolo.PedidoCommit
	if !decodificar(w, r, &corpo, true) {
		return
	}
	if corpo.TxID != "" && corpo.TxID != txID {
		erroHTTP(w, http.StatusBadRequest, "tx_id do corpo difere do tx_id da URL")
		return
	}
	status, resp := Commitar(txID)
	escreverJSON(w, status, resp)
}

func handlerRollback(w http.ResponseWriter, r *http.Request) {
	txID := r.PathValue("tx_id")
	var corpo protocolo.PedidoRollback
	if !decodificar(w, r, &corpo, true) {
		return
	}
	if corpo.TxID != "" && corpo.TxID != txID {
		erroHTTP(w, http.StatusBadRequest, "tx_id do corpo difere do tx_id da URL")
		return
	}
	status, resp := Abortar(txID, corpo.Motivo)
	escreverJSON(w, status, resp)
}
