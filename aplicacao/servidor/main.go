package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/servicos"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/visual"
)

func env(chave, padrao string) string {
	if v := os.Getenv(chave); v != "" {
		return v
	}
	return padrao
}

func envDuracao(chave string, padrao time.Duration) time.Duration {
	if v := os.Getenv(chave); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return padrao
}

func main() {
	tcp := flag.String("tcp", env("TCP_ADDR", cliente_servidor.EnderecoPadrao), "endereco TCP dos clientes (motorista/passageiro)")
	httpAddr := flag.String("http", env("HTTP_ADDR", ":8080"), "endereco HTTP da API REST entre servidores")
	empresa := flag.String("empresa", env("EMPRESA", "VAIJUNTO"), "nome desta empresa no consorcio")
	pares := flag.String("pares", env("PARES", ""), "empresas parceiras: B=http://host:8080,C=http://host:8080")
	dados := flag.String("dados", env("DADOS_DIR", "dados"), "pasta de dados desta empresa")
	ttl := flag.Duration("ttl-bloqueio", envDuracao("TTL_BLOQUEIO", 30*time.Second), "tempo maximo de um assento bloqueado sem commit")
	timeoutPar := flag.Duration("timeout-par", envDuracao("TIMEOUT_PAR", 3*time.Second), "timeout das chamadas HTTP as parceiras")
	flag.Parse()

	if flag.NArg() > 0 {
		*tcp = flag.Arg(0)
	}

	mapaPares, err := servicos.ParsePares(*pares)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("%v", err))
		os.Exit(1)
	}

	servicos.Config = servicos.ConfigConsorcio{
		Empresa:     *empresa,
		Pares:       mapaPares,
		TTLBloqueio: *ttl,
		TimeoutPar:  *timeoutPar,
	}
	servicos.DiretorioDados = *dados

	visual.ExibirCabecalho(fmt.Sprintf("VAIJUNTO - SERVIDOR [%s]", *empresa))

	// Inicializa apenas os ficheiros JSON
	if err := servicos.InicializarCaronas(); err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao inicializar caronas: %v", err))
		return
	}
	if err := servicos.InicializarReservas(); err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao inicializar reservas: %v", err))
		return
	}
	if err := servicos.InicializarTransacoes(); err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao inicializar transacoes: %v", err))
		return
	}

	servicos.AoCancelarCarona = servicos.LimparReservasDaCarona

	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	servicos.IniciarLimpezaBloqueios(ctx)

	// API REST entre servidores
	api := &http.Server{Addr: *httpAddr, Handler: servicos.NovoHandlerAPI(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := api.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			visual.MensagemErro(fmt.Sprintf("API REST encerrou inesperadamente: %v", err))
			os.Exit(1)
		}
	}()

	// Dispara o handshake de reconciliação em segundo plano com os nós parceiros
	go executarAntiEntropia(*empresa, mapaPares)

	listener, err := net.Listen("tcp", *tcp)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Nao foi possivel iniciar o socket TCP em %s: %v", *tcp, err))
		return
	}
	defer listener.Close()

	visual.MensagemSucesso(fmt.Sprintf("Clientes (TCP) em %s | API REST em %s | Parceiras ativas: %d", *tcp, *httpAddr, len(mapaPares)))
	fmt.Println(visual.CinzaClaro + "Pressione Ctrl+C para encerrar o servico com seguranca." + visual.Reset)
	visual.LinhaDivisoria()

	sinais := make(chan os.Signal, 1)
	signal.Notify(sinais, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sinais
		fmt.Println()
		visual.MensagemAviso("Encerrando o servidor VAIJUNTO...")
		_ = listener.Close()
		shutdownCtx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = api.Shutdown(shutdownCtx)
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break
			}
			visual.MensagemErro(fmt.Sprintf("Falha ao aceitar conexao: %v", err))
			continue
		}
		go tratarCliente(conn)
	}

	visual.MensagemSucesso("Servidor finalizado com sucesso. Ate logo!")
}

// executarAntiEntropia realiza o handshake de reconciliação com os servidores do consórcio
func executarAntiEntropia(empresaLocal string, pares map[string]string) {
	// Breve pausa para dar tempo aos contêineres e redes de subirem
	time.Sleep(1 * time.Second)

	ocupados := servicos.InstanciaCaronas.ObterTrechosOcupados()
	if len(ocupados) == 0 {
		return
	}

	clienteHTTP := &http.Client{Timeout: 3 * time.Second}
	corpoReq, err := json.Marshal(modelos.SyncStatusRequest{
		EmpresaSolitante: empresaLocal,
		TrechosAtivos:    ocupados,
	})
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("[Anti-Entropia] Erro ao serializar handshake: %v", err))
		return
	}

	for parNome, urlBase := range pares {
		urlAlvo := fmt.Sprintf("%s/api/v1/sync/status", strings.TrimRight(urlBase, "/"))
		resp, err := clienteHTTP.Post(urlAlvo, "application/json", bytes.NewReader(corpoReq))
		if err != nil {
			// Se o nó estiver indisponível no momento, segue para o próximo par
			continue
		}

		var syncResp modelos.SyncStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&syncResp); err == nil && len(syncResp.TrechosParaLiberar) > 0 {
			qtd := servicos.InstanciaCaronas.LiberarTrechosEmLote(syncResp.TrechosParaLiberar)
			visual.MensagemSucesso(fmt.Sprintf(
				"[Anti-Entropia] Sincronizado com %s: %d trecho(s) cancelado(s) liberado(s) com sucesso!",
				parNome, qtd,
			))
		}
		_ = resp.Body.Close()
	}
}

func tratarCliente(conn net.Conn) {
	remoto := conn.RemoteAddr().String()
	visual.MensagemAviso(fmt.Sprintf("Cliente conectado: %s", remoto))

	defer func() {
		visual.MensagemAviso(fmt.Sprintf("Cliente desconectado: %s", remoto))
		_ = conn.Close()
	}()

	leitor := bufio.NewReader(conn)

	for {
		linha, err := leitor.ReadString('\n')
		if err != nil {
			return
		}

		linhaLimpa := strings.TrimSpace(linha)
		if linhaLimpa == "" {
			continue
		}

		var base struct {
			Tipo string `json:"tipo"`
		}
		if err := json.Unmarshal([]byte(linhaLimpa), &base); err != nil {
			visual.MensagemErro(fmt.Sprintf("Payload malformado de %s: %v", remoto, err))
			continue
		}

		rotearRequisicao(conn, base.Tipo, linhaLimpa, remoto)
	}
}

func rotearRequisicao(conn net.Conn, tipo string, dadosBrutos string, remoto string) {
	switch tipo {

	// Usuários
	case cliente_servidor.TipoCadastroReq:
		servicos.ProcessarCadastro(conn, dadosBrutos)
	case cliente_servidor.TipoLoginReq:
		servicos.ProcessarLogin(conn, dadosBrutos)

	// Caronas
	case cliente_servidor.TipoPublicarCaronaReq:
		servicos.ProcessarPublicarCarona(conn, dadosBrutos)
	case cliente_servidor.TipoConsultarCaronasReq:
		servicos.ProcessarConsultarCaronas(conn, dadosBrutos)
	case cliente_servidor.TipoCancelarCaronaReq:
		servicos.ProcessarCancelarCarona(conn, dadosBrutos)

	// Reservas e itinerários
	case cliente_servidor.TipoBuscarItinerariosReq:
		servicos.ProcessarBuscarItinerarios(conn, dadosBrutos)
	case cliente_servidor.TipoReservarItinerarioReq:
		servicos.ProcessarReservarItinerario(conn, dadosBrutos)
	case cliente_servidor.TipoConsultarReservasReq:
		servicos.ProcessarConsultarReservas(conn, dadosBrutos)
	case cliente_servidor.TipoCancelarReservaReq:
		servicos.ProcessarCancelarReserva(conn, dadosBrutos)

	// Notificações
	case cliente_servidor.TipoConsultarNotificacoesReq:
		servicos.ProcessarConsultarNotificacoes(conn, dadosBrutos)

	default:
		visual.MensagemAviso(fmt.Sprintf("Requisicao desconhecida recebida de %s: '%s'", remoto, tipo))
	}
}
