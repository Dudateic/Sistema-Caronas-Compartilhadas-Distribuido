package service

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
)

// Lado PARTICIPANTE do Two-Phase Commit.
//
// Prepare nao altera AssentosLivres: apenas registra um bloqueio temporario.
// Assim, abortar (rollback ou expiracao) nao exige desfazer nada e um assento
// nunca fica "perdido": o bloqueio simplesmente deixa de existir.
// Commit converte o bloqueio em ocupacao definitiva.
//
// Ordem de locks (evita deadlock): MutexCaronas -> MutexReservas -> mutexTx.

const ArquivoTransacoes = "transacoes.json"

const (
	estadoCommit   = "commit"
	estadoRollback = "rollback"
	estadoExpirada = "expirada"
	estadoFalha    = "falha"
)

type trechoBloqueado struct {
	CaronaID int     `json:"carona_id"`
	Origem   string  `json:"origem"`
	Destino  string  `json:"destino"`
	Preco    float64 `json:"preco"`
}

type transacaoPendente struct {
	TxID       string            `json:"tx_id"`
	Passageiro string            `json:"passageiro"`
	Data       string            `json:"data"`
	Timestamp  int64             `json:"timestamp"`
	Trechos    []trechoBloqueado `json:"trechos"`
	// ExpiraEm usa o relogio LOCAL apenas como timeout (nunca comparado com outros servidores).
	ExpiraEm int64 `json:"expira_em"`
}

type estadoPersistido struct {
	Pendentes   []transacaoPendente `json:"pendentes"`
	Finalizadas map[string]string   `json:"finalizadas"`
}

var (
	mutexTx     sync.Mutex
	pendentes   = map[string]*transacaoPendente{}
	finalizadas = map[string]string{}
)

// InicializarTransacoes recarrega do disco os bloqueios e o historico de transacoes.
// Bloqueios herdados continuam valendo ate expirar, honrando votos SIM ja dados.
func InicializarTransacoes() error {
	var est estadoPersistido
	if err := CarregarJSON(ArquivoTransacoes, &est); err != nil {
		return err
	}
	mutexTx.Lock()
	defer mutexTx.Unlock()
	pendentes = map[string]*transacaoPendente{}
	for i := range est.Pendentes {
		p := est.Pendentes[i]
		pendentes[p.TxID] = &p
	}
	finalizadas = map[string]string{}
	for k, v := range est.Finalizadas {
		finalizadas[k] = v
	}
	return nil
}

func salvarTransacoesLocked() {
	est := estadoPersistido{Pendentes: make([]transacaoPendente, 0, len(pendentes)), Finalizadas: finalizadas}
	for _, p := range pendentes {
		est.Pendentes = append(est.Pendentes, *p)
	}
	if err := SalvarJSON(ArquivoTransacoes, est); err != nil {
		fmt.Printf("[2PC][ERRO] Falha ao persistir transacoes: %v\n", err)
	}
}

// expirarLocked descarta bloqueios vencidos. Deve ser chamado com mutexTx preso.
func expirarLocked(agora int64) int {
	n := 0
	for id, tx := range pendentes {
		if tx.ExpiraEm <= agora {
			delete(pendentes, id)
			finalizadas[id] = estadoExpirada
			n++
			fmt.Printf("[2PC] Bloqueio da transacao %s expirou; assentos liberados\n", id)
		}
	}
	if n > 0 {
		salvarTransacoesLocked()
	}
	return n
}

// LimparBloqueiosExpirados libera os assentos de transacoes que nunca receberam commit.
func LimparBloqueiosExpirados() int {
	mutexTx.Lock()
	defer mutexTx.Unlock()
	return expirarLocked(time.Now().UnixNano())
}

// IniciarLimpezaBloqueios roda a coleta de bloqueios expirados ate o contexto terminar.
func IniciarLimpezaBloqueios(ctx context.Context) {
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				LimparBloqueiosExpirados()
			}
		}
	}()
}

// localizarTrecho exige MutexCaronas preso (leitura ou escrita).
func localizarTrecho(caronaID int, origem, destino string) (*protocolo.Carona, *protocolo.Trecho) {
	for i := range CaronasRegistradas {
		c := &CaronasRegistradas[i]
		if c.ID != caronaID {
			continue
		}
		for j := range c.Trechos {
			t := &c.Trechos[j]
			if t.Origem == origem && t.Destino == destino {
				return c, t
			}
		}
	}
	return nil, nil
}

// bloqueiosLocked conta os bloqueios ativos de um trecho e devolve o de maior precedencia.
func bloqueiosLocked(caronaID int, origem, destino string) (int, *transacaoPendente) {
	n := 0
	var menor *transacaoPendente
	for _, tx := range pendentes {
		for _, t := range tx.Trechos {
			if t.CaronaID == caronaID && t.Origem == origem && t.Destino == destino {
				n++
				if menor == nil || precede(tx.Timestamp, tx.TxID, menor.Timestamp, menor.TxID) {
					menor = tx
				}
			}
		}
	}
	return n, menor
}

// Bloqueados informa quantas vagas de um trecho estao presas por transacoes pendentes.
// Exige que o chamador NAO esteja com mutexTx preso (pode estar com MutexCaronas).
func Bloqueados(caronaID int, origem, destino string) int {
	mutexTx.Lock()
	defer mutexTx.Unlock()
	expirarLocked(time.Now().UnixNano())
	n, _ := bloqueiosLocked(caronaID, origem, destino)
	return n
}

// Preparar executa a fase 1 como participante. Retorna o status HTTP e o voto.
func Preparar(p protocolo.PedidoBloqueio) (int, protocolo.RespostaPrepare) {
	Relogio.Atualizar(p.Timestamp)
	voto := func(status int, ok bool, motivo string) (int, protocolo.RespostaPrepare) {
		return status, protocolo.RespostaPrepare{TxID: p.TxID, Voto: ok, Motivo: motivo, Relogio: Relogio.Valor()}
	}

	MutexCaronas.RLock()
	defer MutexCaronas.RUnlock()
	mutexTx.Lock()
	defer mutexTx.Unlock()

	agora := time.Now().UnixNano()
	expirarLocked(agora)

	if estado, ok := finalizadas[p.TxID]; ok {
		return voto(http.StatusConflict, false, fmt.Sprintf("transacao ja finalizada (%s)", estado))
	}

	carona, trecho := localizarTrecho(p.CaronaID, p.Origem, p.Destino)
	if carona == nil {
		return voto(http.StatusConflict, false, "carona ou trecho inexistente")
	}
	if p.Data != "" && carona.Data != p.Data {
		return voto(http.StatusConflict, false, "data da carona diverge da solicitada")
	}

	tx := pendentes[p.TxID]
	if tx != nil {
		// Repeticao do mesmo prepare (retry de rede): responde SIM sem bloquear de novo.
		for _, h := range tx.Trechos {
			if h.CaronaID == p.CaronaID && h.Origem == p.Origem && h.Destino == p.Destino {
				st, r := voto(http.StatusOK, true, "")
				r.Preco = h.Preco
				return st, r
			}
		}
	}

	if trecho.AssentosLivres <= 0 {
		return voto(http.StatusConflict, false, "assentos esgotados")
	}
	if n, menor := bloqueiosLocked(p.CaronaID, p.Origem, p.Destino); trecho.AssentosLivres-n <= 0 {
		st, r := voto(http.StatusConflict, false, "vaga temporariamente bloqueada por outra transacao")
		r.Bloqueada = true
		if menor != nil {
			r.HolderTimestamp, r.HolderTxID = menor.Timestamp, menor.TxID
		}
		return st, r
	}

	if tx == nil {
		tx = &transacaoPendente{
			TxID:       p.TxID,
			Passageiro: p.Passageiro,
			Data:       p.Data,
			Timestamp:  p.Timestamp,
			ExpiraEm:   agora + Config.TTLBloqueio.Nanoseconds(),
		}
		pendentes[p.TxID] = tx
	}
	tx.Trechos = append(tx.Trechos, trechoBloqueado{CaronaID: p.CaronaID, Origem: p.Origem, Destino: p.Destino, Preco: trecho.Preco})
	salvarTransacoesLocked()

	st, r := voto(http.StatusOK, true, "")
	r.Preco = trecho.Preco
	return st, r
}

// Commitar executa a fase 2 (confirmacao) como participante. E idempotente.
func Commitar(txID string) (int, protocolo.RespostaTransacao) {
	resp := func(status int, estado, msg string) (int, protocolo.RespostaTransacao) {
		return status, protocolo.RespostaTransacao{TxID: txID, Estado: estado, Mensagem: msg}
	}

	MutexCaronas.Lock()
	defer MutexCaronas.Unlock()
	mutexTx.Lock()
	defer mutexTx.Unlock()

	expirarLocked(time.Now().UnixNano())

	if estado, ok := finalizadas[txID]; ok {
		if estado == estadoCommit {
			return resp(http.StatusOK, estadoCommit, "ja confirmada")
		}
		return resp(http.StatusConflict, estado, fmt.Sprintf("transacao nao pode ser confirmada (%s)", estado))
	}

	tx := pendentes[txID]
	if tx == nil {
		return resp(http.StatusNotFound, "desconhecida", "transacao desconhecida neste servidor")
	}

	// Revalida tudo antes de aplicar: ou confirma todos os trechos locais ou nenhum.
	for _, h := range tx.Trechos {
		c, t := localizarTrecho(h.CaronaID, h.Origem, h.Destino)
		if c == nil || t.AssentosLivres <= 0 {
			delete(pendentes, txID)
			finalizadas[txID] = estadoFalha
			salvarTransacoesLocked()
			return resp(http.StatusConflict, estadoFalha, "carona cancelada ou sem assentos no momento do commit")
		}
	}
	for _, h := range tx.Trechos {
		_, t := localizarTrecho(h.CaronaID, h.Origem, h.Destino)
		t.AssentosLivres--
		t.Passageiros = append(t.Passageiros, tx.Passageiro)
	}

	delete(pendentes, txID)
	finalizadas[txID] = estadoCommit
	if err := SalvarJSON(ArquivoCaronas, CaronasRegistradas); err != nil {
		fmt.Printf("[ERRO] Falha ao persistir caronas apos commit: %v\n", err)
	}
	salvarTransacoesLocked()

	fmt.Printf("[2PC] Transacao %s confirmada (%d trecho(s) para '%s')\n", txID, len(tx.Trechos), tx.Passageiro)
	return resp(http.StatusOK, estadoCommit, "")
}

// Abortar executa o rollback como participante. E idempotente e tambem registra
// um marcador para recusar um prepare atrasado que chegue depois do rollback.
func Abortar(txID, motivo string) (int, protocolo.RespostaTransacao) {
	mutexTx.Lock()
	defer mutexTx.Unlock()

	if finalizadas[txID] == estadoCommit {
		return http.StatusConflict, protocolo.RespostaTransacao{TxID: txID, Estado: estadoCommit, Mensagem: "transacao ja confirmada; rollback recusado"}
	}
	if _, ok := pendentes[txID]; ok {
		fmt.Printf("[2PC] Transacao %s abortada (%s); assentos liberados\n", txID, motivo)
	}
	delete(pendentes, txID)
	finalizadas[txID] = estadoRollback
	salvarTransacoesLocked()
	return http.StatusOK, protocolo.RespostaTransacao{TxID: txID, Estado: estadoRollback}
}
