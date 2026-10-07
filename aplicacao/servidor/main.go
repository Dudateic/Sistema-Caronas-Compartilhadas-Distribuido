package main

import (
	"bufio"
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

	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/conexao"
	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
	"Sistema-Caronas-Compartilhadas-Distribuido/service"
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
	tcp := flag.String("tcp", env("TCP_ADDR", conexao.EnderecoPadrao), "endereco TCP dos clientes (motorista/passageiro)")
	httpAddr := flag.String("http", env("HTTP_ADDR", ":8080"), "endereco HTTP da API REST entre servidores")
	empresa := flag.String("empresa", env("EMPRESA", "VAIJUNTO"), "nome desta empresa no consorcio")
	pares := flag.String("pares", env("PARES", ""), "empresas parceiras: B=http://host:8080,C=http://host:8080")
	dados := flag.String("dados", env("DADOS_DIR", "dados"), "pasta de dados desta empresa")
	ttl := flag.Duration("ttl-bloqueio", envDuracao("TTL_BLOQUEIO", 30*time.Second), "tempo maximo de um assento bloqueado sem commit")
	timeoutPar := flag.Duration("timeout-par", envDuracao("TIMEOUT_PAR", 3*time.Second), "timeout das chamadas HTTP as parceiras")
	flag.Parse()
	if flag.NArg() > 0 { // compatibilidade: go run ./aplicacao/servidor :8081
		*tcp = flag.Arg(0)
	}

	mapaPares, err := service.ParsePares(*pares)
	if err != nil {
		fmt.Printf("[ERRO] %v\n", err)
		os.Exit(1)
	}
	service.Config = service.ConfigConsorcio{
		Empresa:     *empresa,
		Pares:       mapaPares,
		TTLBloqueio: *ttl,
		TimeoutPar:  *timeoutPar,
	}
	service.DiretorioDados = *dados

	fmt.Println()
	fmt.Printf("         VAIJUNTO   SERVIDOR  [empresa %s]\n", *empresa)
	fmt.Println()

	// Inicializa os dados persistidos do servidor
	if err := service.InicializarCaronas(); err != nil {
		fmt.Printf("[ERRO] Falha ao inicializar caronas: %v\n", err)
		return
	}
	if err := service.InicializarReservas(); err != nil {
		fmt.Printf("[ERRO] Falha ao inicializar reservas: %v\n", err)
		return
	}
	if err := service.InicializarTransacoes(); err != nil {
		fmt.Printf("[ERRO] Falha ao inicializar transacoes: %v\n", err)
		return
	}

	service.AoCancelarCarona = service.LimparReservasDaCarona

	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	service.IniciarLimpezaBloqueios(ctx)

	// API REST entre servidores (2PC)
	api := &http.Server{Addr: *httpAddr, Handler: service.NovoHandlerAPI(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := api.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("[ERRO] API REST encerrou: %v\n", err)
			os.Exit(1)
		}
	}()

	listener, err := net.Listen("tcp", *tcp)
	if err != nil {
		fmt.Printf("[ERRO] Nao foi possivel iniciar o servidor em %s: %v\n", *tcp, err)
		return
	}
	defer listener.Close()

	fmt.Printf("Clientes (TCP) em %s | API REST em %s | parceiras: %d\n", *tcp, *httpAddr, len(mapaPares))
	fmt.Println("Pressione Ctrl+C para encerrar o servico")
	fmt.Println()

	// Captura interrupcoes do sistema para encerramento
	sinais := make(chan os.Signal, 1)
	signal.Notify(sinais, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sinais
		fmt.Println()
		fmt.Println("Encerrando o servidor VAIJUNTO...")
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
			fmt.Printf("[ERRO] Falha ao aceitar conexao: %v\n", err)
			continue
		}
		go tratarCliente(conn)
	}

	fmt.Println("Servidor finalizado com sucesso. Ate logo!")
}

// tratarCliente atende continuamente as requisicoes de um cliente conectado.
func tratarCliente(conn net.Conn) {
	remoto := conn.RemoteAddr().String()
	fmt.Printf("[CONEXAO] Novo cliente conectado: %s\n", remoto)

	defer func() {
		fmt.Printf("[DESCONEXAO] Cliente desconectado: %s\n", remoto)
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

		var base protocolo.MensagemBase
		if err := json.Unmarshal([]byte(linhaLimpa), &base); err != nil {
			fmt.Printf("[ERRO] Payload malformado de %s: %v\n", remoto, err)
			continue
		}

		service.RegistrarLog(conn, base.Tipo, linhaLimpa)
		rotearRequisicao(conn, base.Tipo, linhaLimpa, remoto)
	}
}

// rotearRequisicao encaminha o JSON para a funcao de processamento.
func rotearRequisicao(conn net.Conn, tipo string, dadosBrutos string, remoto string) {
	switch tipo {

	// Usuarios
	case protocolo.TipoCadastroReq:
		service.ProcessarCadastro(conn, dadosBrutos)
	case protocolo.TipoLoginReq:
		service.ProcessarLogin(conn, dadosBrutos)

	// Caronas (motorista)
	case protocolo.TipoPublicarCaronaReq:
		service.ProcessarPublicarCarona(conn, dadosBrutos)
	case protocolo.TipoConsultarCaronasReq:
		service.ProcessarConsultarCaronas(conn, dadosBrutos)
	case protocolo.TipoCancelarCaronaReq:
		service.ProcessarCancelarCarona(conn, dadosBrutos)

	// Reservas e itinerarios (passageiro)
	case protocolo.TipoBuscarItinerariosReq:
		service.ProcessarBuscarItinerarios(conn, dadosBrutos)
	case protocolo.TipoReservarItinerarioReq:
		service.ProcessarReservarItinerario(conn, dadosBrutos)
	case protocolo.TipoConsultarReservasReq:
		service.ProcessarConsultarReservas(conn, dadosBrutos)
	case protocolo.TipoCancelarReservaReq:
		service.ProcessarCancelarReserva(conn, dadosBrutos)

	// Notificacoes (passageiro)
	case protocolo.TipoConsultarNotificacoesReq:
		service.ProcessarConsultarNotificacoes(conn, dadosBrutos)

	default:
		fmt.Printf("[AVISO] Requisicao desconhecida recebida de %s: '%s'\n", remoto, tipo)
	}
}
