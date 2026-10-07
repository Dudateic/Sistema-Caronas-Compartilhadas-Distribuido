package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
)

// parFalso simula uma empresa parceira com comportamento controlado pelo teste.
type parFalso struct {
	srv *httptest.Server

	mu            sync.Mutex
	chamadas      []string
	votos         []func() (int, protocolo.RespostaPrepare) // um por prepare; o ultimo se repete
	atrasoPrepare time.Duration
	falhasCommit  int
}

func novoParFalso(t *testing.T) *parFalso {
	t.Helper()
	p := &parFalso{}
	mux := http.NewServeMux()

	mux.HandleFunc("POST "+protocolo.RotaPrepare, func(w http.ResponseWriter, r *http.Request) {
		var ped protocolo.PedidoBloqueio
		_ = json.NewDecoder(r.Body).Decode(&ped)
		p.mu.Lock()
		p.chamadas = append(p.chamadas, "prepare")
		atraso := p.atrasoPrepare
		var voto func() (int, protocolo.RespostaPrepare)
		if len(p.votos) > 0 {
			voto = p.votos[0]
			if len(p.votos) > 1 {
				p.votos = p.votos[1:]
			}
		}
		p.mu.Unlock()

		time.Sleep(atraso)
		status, resp := http.StatusOK, protocolo.RespostaPrepare{Voto: true, Preco: 20}
		if voto != nil {
			status, resp = voto()
		}
		resp.TxID = ped.TxID
		resp.Relogio = ped.Timestamp + 1
		escreverJSON(w, status, resp)
	})
	mux.HandleFunc("POST /api/v1/transacoes/{tx_id}/commit", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.chamadas = append(p.chamadas, "commit")
		falha := p.falhasCommit > 0
		if falha {
			p.falhasCommit--
		}
		p.mu.Unlock()
		if falha {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		escreverJSON(w, 200, protocolo.RespostaTransacao{TxID: r.PathValue("tx_id"), Estado: "commit"})
	})
	mux.HandleFunc("POST /api/v1/transacoes/{tx_id}/rollback", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.chamadas = append(p.chamadas, "rollback")
		p.mu.Unlock()
		escreverJSON(w, 200, protocolo.RespostaTransacao{TxID: r.PathValue("tx_id"), Estado: "rollback"})
	})

	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *parFalso) conta(tipo string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, c := range p.chamadas {
		if c == tipo {
			n++
		}
	}
	return n
}

func (p *parFalso) esperar(t *testing.T, tipo string, minimo int) {
	t.Helper()
	limite := time.Now().Add(5 * time.Second)
	for time.Now().Before(limite) {
		if p.conta(tipo) >= minimo {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("o par nao recebeu %d chamada(s) de %s (recebeu %d)", minimo, tipo, p.conta(tipo))
}

func votoNao(motivo string) func() (int, protocolo.RespostaPrepare) {
	return func() (int, protocolo.RespostaPrepare) {
		return http.StatusConflict, protocolo.RespostaPrepare{Voto: false, Motivo: motivo}
	}
}

func votoBloqueado(holderTS int64, holderTx string) func() (int, protocolo.RespostaPrepare) {
	return func() (int, protocolo.RespostaPrepare) {
		return http.StatusConflict, protocolo.RespostaPrepare{
			Voto: false, Motivo: "bloqueada", Bloqueada: true, HolderTimestamp: holderTS, HolderTxID: holderTx,
		}
	}
}

func votoSim() func() (int, protocolo.RespostaPrepare) {
	return func() (int, protocolo.RespostaPrepare) {
		return http.StatusOK, protocolo.RespostaPrepare{Voto: true, Preco: 20}
	}
}

// cenario prepara uma empresa local ("VAIJUNTO") com 1 assento e uma parceira "B" falsa.
func cenario(t *testing.T) (localID int, par *parFalso, trechoLocal, trechoRemoto protocolo.TrechoViagem) {
	t.Helper()
	resetar(t)
	t.Cleanup(retriesDecisao.Wait)
	par = novoParFalso(t)
	Config.Pares = map[string]string{"B": par.srv.URL}
	Config.TimeoutPar = 300 * time.Millisecond
	localID = publicar(t, "m1", []string{"A", "M"}, "08:00", 1, 10)
	trechoLocal = trechoViagem(localID, "A", "M", "08:00", 10)
	trechoRemoto = protocolo.TrechoViagem{Empresa: "B", CaronaID: 99, Origem: "M", Destino: "Z", Horario: "09:00"}
	return
}

func TestCoordenadorTodosVotamSim(t *testing.T) {
	id, par, local, remoto := cenario(t)

	r := reservar(t, "p1", local, remoto)
	if !r.Sucesso || r.ReservaID == 0 {
		t.Fatalf("reserva deveria ser confirmada: %+v", r)
	}
	if par.conta("prepare") != 1 || par.conta("commit") != 1 || par.conta("rollback") != 0 {
		t.Fatalf("chamadas ao par inesperadas: %v", par.chamadas)
	}
	if l := livres(id, "A", "M"); l != 0 {
		t.Fatalf("trecho local deveria estar ocupado, livres=%d", l)
	}
	MutexReservas.Lock()
	defer MutexReservas.Unlock()
	if len(ReservasRegistradas) != 1 {
		t.Fatalf("esperava 1 reserva, veio %d", len(ReservasRegistradas))
	}
	res := ReservasRegistradas[0]
	if res.TxID == "" || res.PrecoTotal != 30 || len(res.Trechos) != 2 {
		t.Fatalf("reserva incompleta (preco deve somar os votos 10+20): %+v", res)
	}
	if n := qtdPendentes(); n != 0 {
		t.Fatalf("restaram %d bloqueios locais", n)
	}
}

func TestCoordenadorParVotaNaoAbortaTudo(t *testing.T) {
	id, par, local, remoto := cenario(t)
	par.votos = []func() (int, protocolo.RespostaPrepare){votoNao("assentos esgotados")}

	r := reservar(t, "p1", local, remoto)
	if r.Sucesso {
		t.Fatal("reserva nao deveria ser confirmada")
	}
	if par.conta("commit") != 0 || par.conta("rollback") != 1 {
		t.Fatalf("par deveria receber rollback e nenhum commit: %v", par.chamadas)
	}
	if l := livres(id, "A", "M"); l != 1 {
		t.Fatalf("trecho local nao pode ficar ocupado, livres=%d", l)
	}
	if n := qtdPendentes(); n != 0 {
		t.Fatalf("bloqueio local vazou: %d pendentes", n)
	}
	MutexReservas.Lock()
	defer MutexReservas.Unlock()
	if len(ReservasRegistradas) != 0 {
		t.Fatal("nenhuma reserva deveria ter sido gravada")
	}
}

func TestCoordenadorParForaDoAr(t *testing.T) {
	id, par, local, remoto := cenario(t)
	par.srv.Close()

	inicio := time.Now()
	r := reservar(t, "p1", local, remoto)
	if r.Sucesso {
		t.Fatal("reserva nao deveria ser confirmada com o par fora do ar")
	}
	if d := time.Since(inicio); d > 3*time.Second {
		t.Fatalf("falha demorou %v: o timeout deveria detectar rapido", d)
	}
	if l := livres(id, "A", "M"); l != 1 {
		t.Fatalf("trecho local nao pode ficar ocupado, livres=%d", l)
	}
	if n := qtdPendentes(); n != 0 {
		t.Fatalf("bloqueio local vazou: %d pendentes", n)
	}
	// A empresa local continua operando para caronas que nao dependem do par.
	if !reservar(t, "p2", local).Sucesso {
		t.Fatal("reserva apenas local deveria funcionar com o par fora do ar")
	}
}

func TestCoordenadorParLentoViraTimeoutERollback(t *testing.T) {
	id, par, local, remoto := cenario(t)
	par.atrasoPrepare = 800 * time.Millisecond // maior que TimeoutPar (300ms)

	r := reservar(t, "p1", local, remoto)
	if r.Sucesso {
		t.Fatal("reserva nao deveria ser confirmada com o par lento")
	}
	// O par pode ter bloqueado a vaga sem que o SIM chegasse: o rollback precisa ir mesmo assim.
	par.esperar(t, "rollback", 1)
	if par.conta("commit") != 0 {
		t.Fatal("nao pode haver commit")
	}
	if l := livres(id, "A", "M"); l != 1 {
		t.Fatalf("livres=%d, esperado 1", l)
	}
}

func TestCoordenadorRepeteCommitAteOParResponder(t *testing.T) {
	_, par, local, remoto := cenario(t)
	par.falhasCommit = 2

	r := reservar(t, "p1", local, remoto)
	if !r.Sucesso {
		t.Fatalf("decisao ja era commit; o passageiro deve receber sucesso: %+v", r)
	}
	par.esperar(t, "commit", 3) // 1 tentativa + 2 reenvios
}

func TestCoordenadorEmpresaDesconhecida(t *testing.T) {
	_, par, local, _ := cenario(t)
	r := reservar(t, "p1", local, protocolo.TrechoViagem{Empresa: "Z", CaronaID: 1, Origem: "M", Destino: "Z"})
	if r.Sucesso {
		t.Fatal("empresa fora do consorcio deveria ser recusada")
	}
	if par.conta("prepare") != 0 {
		t.Fatal("nao deveria contatar ninguem antes de validar o itinerario")
	}
	if n := qtdPendentes(); n != 0 {
		t.Fatalf("bloqueio vazou: %d", n)
	}
}

func TestCoordenadorRecusaTrechoRepetido(t *testing.T) {
	_, _, local, _ := cenario(t)
	if r := reservar(t, "p1", local, local); r.Sucesso {
		t.Fatal("itinerario repetindo o trecho deveria ser recusado")
	}
}

// wait-die: a transacao mais antiga espera a vaga liberar e tenta de novo.
func TestCoordenadorMaisAntigaEspera(t *testing.T) {
	_, par, local, remoto := cenario(t)
	holderMaisNovo := votoBloqueado(1<<60, "zzz")
	par.votos = []func() (int, protocolo.RespostaPrepare){holderMaisNovo, holderMaisNovo, votoSim()}

	r := reservar(t, "p1", local, remoto)
	if !r.Sucesso {
		t.Fatalf("a transacao mais antiga deveria esperar e conseguir: %+v", r)
	}
	if n := par.conta("prepare"); n != 3 {
		t.Fatalf("esperava 3 prepares (2 esperas + 1 SIM), veio %d", n)
	}
}

// wait-die: a transacao mais nova aborta imediatamente, sem esperar nem causar deadlock.
func TestCoordenadorMaisNovaAbortaNaHora(t *testing.T) {
	_, par, local, remoto := cenario(t)
	par.votos = []func() (int, protocolo.RespostaPrepare){votoBloqueado(0, "a")}

	r := reservar(t, "p1", local, remoto)
	if r.Sucesso {
		t.Fatal("a transacao mais nova deveria abortar")
	}
	if n := par.conta("prepare"); n != 1 {
		t.Fatalf("mais nova nao deve repetir o prepare, houve %d", n)
	}
	if n := qtdPendentes(); n != 0 {
		t.Fatalf("bloqueio local vazou: %d", n)
	}
}

func TestCoordenadorNuncaTratar409ComoSim(t *testing.T) {
	_, par, local, remoto := cenario(t)
	// Par mal comportado: status 409 mas corpo dizendo voto=true.
	par.votos = []func() (int, protocolo.RespostaPrepare){func() (int, protocolo.RespostaPrepare) {
		return http.StatusConflict, protocolo.RespostaPrepare{Voto: true, Preco: 20}
	}}
	if r := reservar(t, "p1", local, remoto); r.Sucesso {
		t.Fatal("HTTP 409 deve valer como NAO mesmo que o corpo diga voto=true")
	}
}
