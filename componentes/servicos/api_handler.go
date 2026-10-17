package servicos

import (
	"context"
	"net"
	"net/http"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/distribuido"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/rest"
)

var (
	AoCancelarCarona      func(caronaID int)
	InstanciaCaronas      *GerenciadorCaronas
	InstanciaReservas     *GerenciadorReservas
	InstanciaUsuarios     *GerenciadorUsuarios
	InstanciaWAL          *distribuido.LogTransacoes
	InstanciaParticipante *distribuido.Participante2PC
	InstanciaCoordenador  *distribuido.Coordenador2PC
	InstanciaClienteREST  *rest.ClienteRESTInterEmpresas
)

func InicializarCaronas() error {
	var err error
	InstanciaCaronas, err = NovoGerenciadorCaronas(Config.Empresa, DiretorioDados)
	return err
}

func InicializarReservas() error {
	InstanciaReservas = NovoGerenciadorReservas(Config.Empresa, DiretorioDados)
	InstanciaUsuarios = NovoGerenciadorUsuarios(DiretorioDados)
	return nil
}

func InicializarTransacoes() error {
	var err error
	InstanciaWAL, err = distribuido.NovoLogTransacoes(DiretorioDados + "/log_transacoes.log")
	if err != nil {
		return err
	}

	InstanciaParticipante, err = distribuido.NovoParticipante2PC(Config.Empresa, InstanciaCaronas, InstanciaWAL)
	if err != nil {
		return err
	}

	InstanciaClienteREST = rest.NovoClienteREST(Config.TimeoutPar)
	InstanciaCoordenador = distribuido.NovoCoordenador2PC(
		Config.Empresa,
		"", // URL local
		InstanciaClienteREST,
		InstanciaWAL,
		InstanciaParticipante,
		Config.Pares,
	)

	return nil
}

func LimparReservasDaCarona(caronaID int) {
	// Notifica e cancela eventuais reservas atreladas
}

func IniciarLimpezaBloqueios(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(Config.TTLBloqueio / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if InstanciaParticipante != nil {
					InstanciaParticipante.LimparTransacoesExpiradas(Config.TTLBloqueio)
				}
			}
		}
	}()
}

func NovoHandlerAPI() http.Handler {
	mux := http.NewServeMux()
	handlers := rest.NovoHandlersREST(Config.Empresa, InstanciaCaronas, InstanciaWAL)
	handlers.RegistrarRotas(mux)
	return mux
}

func RegistrarLog(conn net.Conn, tipo, payload string) {
	// Log de auditoria simples
}
