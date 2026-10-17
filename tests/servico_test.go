package testes

import (
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/servicos"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
)

// TestMain isola a persistencia em um diretorio temporario para nao tocar nos dados reais.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "vaijunto-test-")
	if err != nil {
		fmt.Println("falha ao criar diretorio temporario:", err)
		os.Exit(1)
	}
	if err := os.Chdir(tmp); err != nil {
		fmt.Println("falha ao entrar no diretorio temporario:", err)
		os.Exit(1)
	}
	_ = os.MkdirAll(service.DiretorioDados, 0755)
	esperaPrepare = 5 * time.Millisecond
	intervaloDecisao = 20 * time.Millisecond
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

// resetar devolve o estado em memoria e em disco ao inicio.
func resetar(t *testing.T) {
	t.Helper()
	retriesDecisao.Wait() // reenvios em segundo plano de testes anteriores
	mutexTx.Lock()
	pendentes = map[string]*transacaoPendente{}
	finalizadas = map[string]string{}
	mutexTx.Unlock()
	service.Config = service.ConfigConsorcio{Empresa: "VAIJUNTO", Pares: map[string]string{}, TTLBloqueio: 30 * time.Second, TimeoutPar: 3 * time.Second}
	service.MutexCaronas.Lock()
	service.MutexReservas.Lock()
	service.CaronasRegistradas = nil
	service.ProximoCaronaID = 1
	service.ReservasRegistradas = nil
	service.ProximoReservaID = 1
	service.NotificacoesRegistradas = nil
	service.AoCancelarCarona = nil
	service.MutexReservas.Unlock()
	service.MutexCaronas.Unlock()

	entradas, _ := os.ReadDir(service.DiretorioDados)
	for _, e := range entradas {
		_ = os.Remove(service.DiretorioDados + "/" + e.Name())
	}
}

// chamar executa um handler do servidor sobre uma conexao em memoria e decodifica a resposta.
func chamar(t *testing.T, handler func(net.Conn, string), req any, resp any) {
	t.Helper()
	bruto, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	servidor, cliente := net.Pipe()
	defer cliente.Close()
	go func() {
		defer servidor.Close()
		handler(servidor, string(bruto))
	}()
	_ = cliente.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err := json.NewDecoder(cliente).Decode(resp); err != nil {
		t.Fatalf("decodificando resposta: %v", err)
	}
}

func publicar(t *testing.T, motorista string, rota []string, horario string, assentos int, preco float64) int {
	t.Helper()
	var r protocolo.RespostaPublicarCarona
	chamar(t, service.ProcessarPublicarCarona, protocolo.PublicacaoCarona{
		Tipo:           protocolo.TipoPublicarCaronaReq,
		Motorista:      motorista,
		Rota:           rota,
		Data:           "2030-01-01",
		Horario:        horario,
		AssentosTotais: assentos,
		PrecoPorTrecho: preco,
	}, &r)
	if !r.Sucesso {
		t.Fatalf("publicar falhou: %s", r.Mensagem)
	}
	return r.CaronaID
}

func trechoViagem(caronaID int, origem, destino, horario string, preco float64) protocolo.TrechoViagem {
	return protocolo.TrechoViagem{Empresa: "VAIJUNTO", CaronaID: caronaID, Origem: origem, Destino: destino, Horario: horario, Preco: preco}
}

func reservar(t *testing.T, passageiro string, trechos ...protocolo.TrechoViagem) protocolo.RespostaReservar {
	t.Helper()
	var r protocolo.RespostaReservar
	chamar(t, service.ProcessarReservarItinerario, protocolo.SolicitacaoReserva{
		Tipo:       protocolo.TipoReservarItinerarioReq,
		Passageiro: passageiro,
		Data:       "2030-01-01",
		Trechos:    trechos,
	}, &r)
	return r
}

func livres(caronaID int, origem, destino string) int {
	service.MutexCaronas.RLock()
	defer service.MutexCaronas.RUnlock()
	for _, c := range service.CaronasRegistradas {
		if c.ID != caronaID {
			continue
		}
		for _, tr := range c.Trechos {
			if tr.Origem == origem && tr.Destino == destino {
				return tr.AssentosLivres
			}
		}
	}
	return -1
}

// verificarInvariantes confere a consistencia global: nenhum trecho com assentos
// negativos ou superlotado, e ocupacao coerente com a lista de passageiros.
func verificarInvariantes(t *testing.T) {
	t.Helper()
	service.MutexCaronas.RLock()
	defer service.MutexCaronas.RUnlock()
	for _, c := range service.CaronasRegistradas {
		for _, tr := range c.Trechos {
			if tr.AssentosLivres < 0 {
				t.Errorf("carona %d trecho %s->%s com assentos negativos (%d)", c.ID, tr.Origem, tr.Destino, tr.AssentosLivres)
			}
			if ocupados := c.AssentosTotais - tr.AssentosLivres; ocupados != len(tr.Passageiros) {
				t.Errorf("carona %d trecho %s->%s: %d ocupados mas %d passageiros listados",
					c.ID, tr.Origem, tr.Destino, ocupados, len(tr.Passageiros))
			}
		}
	}
}

func TestPublicarCaronaMontaTrechos(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B", "C"}, "08:00", 3, 10)

	service.MutexCaronas.RLock()
	defer service.MutexCaronas.RUnlock()
	if len(service.CaronasRegistradas) != 1 || service.CaronasRegistradas[0].ID != id {
		t.Fatalf("carona nao registrada corretamente: %+v", service.CaronasRegistradas)
	}
	if n := len(service.CaronasRegistradas[0].Trechos); n != 2 {
		t.Fatalf("esperava 2 trechos, veio %d", n)
	}
}

func TestPublicarCaronaValidacoes(t *testing.T) {
	resetar(t)
	casos := map[string]protocolo.PublicacaoCarona{
		"rota curta":     {Rota: []string{"A"}, AssentosTotais: 1, PrecoPorTrecho: 1},
		"sem assentos":   {Rota: []string{"A", "B"}, AssentosTotais: 0, PrecoPorTrecho: 1},
		"preco invalido": {Rota: []string{"A", "B"}, AssentosTotais: 1, PrecoPorTrecho: 0},
	}
	for nome, req := range casos {
		req.Tipo = protocolo.TipoPublicarCaronaReq
		var r protocolo.RespostaPublicarCarona
		chamar(t, service.ProcessarPublicarCarona, req, &r)
		if r.Sucesso {
			t.Errorf("%s: publicacao deveria ser recusada", nome)
		}
	}
}

// Requisito 9.1: nenhum assento confirmado para mais de um passageiro.
func TestUltimaVagaConcorrente(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 1, 10)

	const disputantes = 60
	var wg sync.WaitGroup
	var mu sync.Mutex
	sucessos := 0
	for i := 0; i < disputantes; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := reservar(t, fmt.Sprintf("p%d", i), trechoViagem(id, "A", "B", "08:00", 10))
			if r.Sucesso {
				mu.Lock()
				sucessos++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if sucessos != 1 {
		t.Fatalf("esperava exatamente 1 reserva para a ultima vaga, vieram %d", sucessos)
	}
	if l := livres(id, "A", "B"); l != 0 {
		t.Fatalf("esperava 0 assentos livres, veio %d", l)
	}
	service.MutexReservas.Lock()
	n := len(service.ReservasRegistradas)
	service.MutexReservas.Unlock()
	if n != 1 {
		t.Fatalf("esperava 1 reserva registrada, veio %d", n)
	}
	verificarInvariantes(t)
}

// Contencao alta com varios assentos: nunca confirma mais reservas do que vagas.
func TestContencaoNaoExcedeCapacidade(t *testing.T) {
	resetar(t)
	const assentos = 5
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", assentos, 10)

	const disputantes = 200
	var wg sync.WaitGroup
	var mu sync.Mutex
	sucessos := 0
	for i := 0; i < disputantes; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if reservar(t, fmt.Sprintf("p%d", i), trechoViagem(id, "A", "B", "08:00", 10)).Sucesso {
				mu.Lock()
				sucessos++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if sucessos != assentos {
		t.Fatalf("esperava %d reservas, vieram %d", assentos, sucessos)
	}
	verificarInvariantes(t)
}

// Requisito 9.2: itinerario confirmado integralmente ou nao confirmado.
func TestReservaAtomicaNaoDeixaParcial(t *testing.T) {
	resetar(t)
	c1 := publicar(t, "m1", []string{"A", "B"}, "08:00", 2, 10)
	c2 := publicar(t, "m2", []string{"B", "C"}, "09:00", 1, 20)

	// Esgota a carona 2.
	if r := reservar(t, "p0", trechoViagem(c2, "B", "C", "09:00", 20)); !r.Sucesso {
		t.Fatalf("reserva inicial falhou: %s", r.Mensagem)
	}

	// Itinerario A->B->C deve falhar inteiro e nao consumir vaga da carona 1.
	r := reservar(t, "p1",
		trechoViagem(c1, "A", "B", "08:00", 10),
		trechoViagem(c2, "B", "C", "09:00", 20),
	)
	if r.Sucesso {
		t.Fatal("itinerario com trecho esgotado nao deveria ser confirmado")
	}
	if l := livres(c1, "A", "B"); l != 2 {
		t.Fatalf("trecho do itinerario recusado ficou preso: livres=%d, esperado 2", l)
	}
	verificarInvariantes(t)
}

// Atomicidade sob concorrencia: itinerarios de dois trechos disputando vagas limitadas.
func TestReservaAtomicaSobConcorrencia(t *testing.T) {
	resetar(t)
	c1 := publicar(t, "m1", []string{"A", "B"}, "08:00", 10, 10)
	c2 := publicar(t, "m2", []string{"B", "C"}, "09:00", 3, 20)

	const clientes = 100
	var wg sync.WaitGroup
	var mu sync.Mutex
	sucessos := 0
	for i := 0; i < clientes; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := reservar(t, fmt.Sprintf("p%d", i),
				trechoViagem(c1, "A", "B", "08:00", 10),
				trechoViagem(c2, "B", "C", "09:00", 20),
			)
			if r.Sucesso {
				mu.Lock()
				sucessos++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	// Seguranca: nunca mais que a capacidade do trecho mais escasso. Em contencao alta o
	// 2PC pode abortar itinerarios mesmo havendo vaga (wait-die), por isso so se exige <= 3.
	if sucessos > 3 {
		t.Fatalf("confirmou %d itinerarios, mas a carona 2 so tem 3 vagas", sucessos)
	}
	if sucessos == 0 {
		t.Fatal("nenhum itinerario confirmado: houve vaga e nao houve progresso")
	}
	// Atomicidade: os dois trechos tem exatamente a mesma ocupacao (nada parcial).
	if o1, o2 := 10-livres(c1, "A", "B"), 3-livres(c2, "B", "C"); o1 != sucessos || o2 != sucessos {
		t.Fatalf("ocupacao inconsistente: carona1=%d carona2=%d confirmados=%d", o1, o2, sucessos)
	}
	verificarInvariantes(t)
}

func TestCancelarReservaLiberaAssentos(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 1, 10)
	r := reservar(t, "p1", trechoViagem(id, "A", "B", "08:00", 10))
	if !r.Sucesso {
		t.Fatalf("reserva falhou: %s", r.Mensagem)
	}

	// Outro passageiro nao pode cancelar a reserva alheia.
	var neg protocolo.RespostaSimples
	chamar(t, service.ProcessarCancelarReserva, protocolo.CancelamentoReserva{
		Tipo: protocolo.TipoCancelarReservaReq, ReservaID: r.ReservaID, Passageiro: "intruso",
	}, &neg)
	if neg.Sucesso {
		t.Fatal("passageiro conseguiu cancelar reserva de outro")
	}
	if l := livres(id, "A", "B"); l != 0 {
		t.Fatalf("tentativa indevida alterou os assentos: livres=%d", l)
	}

	var ok protocolo.RespostaSimples
	chamar(t, service.ProcessarCancelarReserva, protocolo.CancelamentoReserva{
		Tipo: protocolo.TipoCancelarReservaReq, ReservaID: r.ReservaID, Passageiro: "p1",
	}, &ok)
	if !ok.Sucesso {
		t.Fatalf("cancelamento falhou: %s", ok.Mensagem)
	}
	if l := livres(id, "A", "B"); l != 1 {
		t.Fatalf("assento nao foi liberado: livres=%d", l)
	}
	// A vaga liberada pode ser reservada de novo.
	if !reservar(t, "p2", trechoViagem(id, "A", "B", "08:00", 10)).Sucesso {
		t.Fatal("vaga liberada nao pode ser reutilizada")
	}
	verificarInvariantes(t)
}

func TestCancelarCaronaCancelaReservasENotifica(t *testing.T) {
	resetar(t)
	service.AoCancelarCarona = service.LimparReservasDaCarona
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 2, 10)
	if !reservar(t, "p1", trechoViagem(id, "A", "B", "08:00", 10)).Sucesso {
		t.Fatal("reserva falhou")
	}

	var r protocolo.RespostaSimples
	chamar(t, service.ProcessarCancelarCarona, protocolo.CancelamentoCarona{
		Tipo: protocolo.TipoCancelarCaronaReq, CaronaID: id, Motorista: "m1",
	}, &r)
	if !r.Sucesso {
		t.Fatalf("cancelamento da carona falhou: %s", r.Mensagem)
	}

	// A limpeza roda em goroutine: aguarda a notificacao chegar.
	var notif protocolo.RespostaConsultarNotificacoes
	limite := time.Now().Add(5 * time.Second)
	for time.Now().Before(limite) {
		chamar(t, service.ProcessarConsultarNotificacoes, protocolo.ConsultaNotificacoes{
			Tipo: protocolo.TipoConsultarNotificacoesReq, Passageiro: "p1",
		}, &notif)
		if len(notif.Notificacoes) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(notif.Notificacoes) != 1 {
		t.Fatalf("esperava 1 notificacao para p1, veio %d", len(notif.Notificacoes))
	}

	var lista protocolo.RespostaConsultarReservas
	chamar(t, service.ProcessarConsultarReservas, protocolo.ConsultaReservasPassageiro{
		Tipo: protocolo.TipoConsultarReservasReq, Passageiro: "p1",
	}, &lista)
	if len(lista.Reservas) != 0 {
		t.Fatalf("reserva deveria ter sido removida, restam %d", len(lista.Reservas))
	}

	// Notificacao e entregue uma unica vez.
	chamar(t, service.ProcessarConsultarNotificacoes, protocolo.ConsultaNotificacoes{
		Tipo: protocolo.TipoConsultarNotificacoesReq, Passageiro: "p1",
	}, &notif)
	if len(notif.Notificacoes) != 0 {
		t.Fatalf("notificacao foi entregue mais de uma vez")
	}
}

func TestCancelarCaronaDeOutroMotoristaNegado(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 1, 10)
	var r protocolo.RespostaSimples
	chamar(t, service.ProcessarCancelarCarona, protocolo.CancelamentoCarona{
		Tipo: protocolo.TipoCancelarCaronaReq, CaronaID: id, Motorista: "outro",
	}, &r)
	if r.Sucesso {
		t.Fatal("motorista cancelou carona alheia")
	}
	service.MutexCaronas.RLock()
	n := len(service.CaronasRegistradas)
	service.MutexCaronas.RUnlock()
	if n != 1 {
		t.Fatalf("carona deveria continuar registrada, restam %d", n)
	}
}

func TestBuscaItinerarioComConexao(t *testing.T) {
	resetar(t)
	publicar(t, "m1", []string{"A", "B"}, "08:00", 2, 10)
	publicar(t, "m2", []string{"B", "C"}, "09:00", 2, 20)
	// Conexao impossivel: sai antes de a primeira chegar.
	publicar(t, "m3", []string{"B", "C"}, "07:00", 2, 5)

	var r protocolo.RespostaBuscarItinerarios
	chamar(t, service.ProcessarBuscarItinerarios, protocolo.ConsultaItinerario{
		Tipo: protocolo.TipoBuscarItinerariosReq, Origem: "A", Destino: "C", Data: "2030-01-01",
	}, &r)

	if len(r.Itinerarios) != 1 {
		t.Fatalf("esperava 1 itinerario valido, vieram %d: %+v", len(r.Itinerarios), r.Itinerarios)
	}
	it := r.Itinerarios[0]
	if len(it.Trechos) != 2 || it.PrecoTotal != 30 {
		t.Fatalf("itinerario inesperado: %+v", it)
	}
}

func TestBuscaIgnoraTrechoLotadoEOutraData(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 1, 10)
	if !reservar(t, "p1", trechoViagem(id, "A", "B", "08:00", 10)).Sucesso {
		t.Fatal("reserva falhou")
	}

	var r protocolo.RespostaBuscarItinerarios
	chamar(t, service.ProcessarBuscarItinerarios, protocolo.ConsultaItinerario{
		Tipo: protocolo.TipoBuscarItinerariosReq, Origem: "A", Destino: "B", Data: "2030-01-01",
	}, &r)
	if len(r.Itinerarios) != 0 {
		t.Fatalf("trecho lotado nao deveria aparecer na busca: %+v", r.Itinerarios)
	}

	chamar(t, service.ProcessarBuscarItinerarios, protocolo.ConsultaItinerario{
		Tipo: protocolo.TipoBuscarItinerariosReq, Origem: "A", Destino: "B", Data: "2031-02-02",
	}, &r)
	if len(r.Itinerarios) != 0 {
		t.Fatalf("data diferente nao deveria retornar itinerarios")
	}
}

// Estado persistido em disco precisa sobreviver a reinicio (recuperacao apos queda).
func TestPersistenciaAposReinicio(t *testing.T) {
	resetar(t)
	id := publicar(t, "m1", []string{"A", "B"}, "08:00", 2, 10)
	rv := reservar(t, "p1", trechoViagem(id, "A", "B", "08:00", 10))
	if !rv.Sucesso {
		t.Fatal("reserva falhou")
	}

	// Simula reinicio: zera memoria e recarrega do disco.
	service.MutexCaronas.Lock()
	service.MutexReservas.Lock()
	service.CaronasRegistradas = nil
	service.ProximoCaronaID = 1
	service.ReservasRegistradas = nil
	service.ProximoReservaID = 1
	service.MutexReservas.Unlock()
	service.MutexCaronas.Unlock()

	if err := service.InicializarCaronas(); err != nil {
		t.Fatal(err)
	}
	if err := service.InicializarReservas(); err != nil {
		t.Fatal(err)
	}

	if l := livres(id, "A", "B"); l != 1 {
		t.Fatalf("apos reinicio esperava 1 assento livre, veio %d", l)
	}
	service.MutexReservas.Lock()
	n, prox := len(service.ReservasRegistradas), service.ProximoReservaID
	service.MutexReservas.Unlock()
	if n != 1 || prox != rv.ReservaID+1 {
		t.Fatalf("reservas nao recuperadas: n=%d proximoID=%d", n, prox)
	}
	verificarInvariantes(t)
}

func TestCadastroELogin(t *testing.T) {
	resetar(t)
	cad := func(usuario, senha, perfil string) protocolo.RespostaSimples {
		var r protocolo.RespostaSimples
		chamar(t, service.ProcessarCadastro, protocolo.RegistroUsuario{
			Tipo: protocolo.TipoCadastroReq, Usuario: usuario, Senha: senha, TipoUsuario: perfil,
		}, &r)
		return r
	}
	if r := cad("ana", "123", protocolo.PerfilPassageiro); !r.Sucesso {
		t.Fatalf("cadastro falhou: %s", r.Mensagem)
	}
	if r := cad("ana", "outra", protocolo.PerfilMotorista); r.Sucesso {
		t.Fatal("usuario duplicado deveria ser recusado")
	}
	if r := cad("bob", "123", "admin"); r.Sucesso {
		t.Fatal("perfil invalido deveria ser recusado")
	}

	login := func(usuario, senha string) protocolo.RespostaLogin {
		var r protocolo.RespostaLogin
		chamar(t, service.ProcessarLogin, protocolo.CredencialLogin{
			Tipo: protocolo.TipoLoginReq, Usuario: usuario, Senha: senha,
		}, &r)
		return r
	}
	if r := login("ana", "123"); !r.Sucesso || r.TipoUsuario != protocolo.PerfilPassageiro {
		t.Fatalf("login valido falhou: %+v", r)
	}
	if r := login("ana", "errada"); r.Sucesso {
		t.Fatal("senha errada nao deveria autenticar")
	}
}
