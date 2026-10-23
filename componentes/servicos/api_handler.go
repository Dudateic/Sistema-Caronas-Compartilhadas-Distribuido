package servicos

import (
	"context"
	"fmt"
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

	InstanciaParticipante, err = distribuido.NovoParticipante2PC(Config.Empresa, InstanciaCaronas, nil)
	if err != nil {
		return err
	}

	InstanciaClienteREST = rest.NovoClienteREST(Config.TimeoutPar)
	InstanciaCoordenador = distribuido.NovoCoordenador2PC(
		Config.Empresa,
		"",
		InstanciaClienteREST,
		nil,
		InstanciaParticipante,
		Config.Pares,
	)

	return nil
}

func LimparReservasDaCarona(caronaID int) {
	if InstanciaReservas == nil {
		return
	}

	InstanciaReservas.mu.Lock()
	defer InstanciaReservas.mu.Unlock()

	alterouReservas := false
	alterouNotificacoes := false

	for id, res := range InstanciaReservas.reservas {
		afetada := false
		for _, t := range res.Trechos {
			if t.CaronaID == caronaID && (t.Empresa == Config.Empresa || t.Empresa == "") {
				afetada = true
				break
			}
		}

		if afetada {
			delete(InstanciaReservas.reservas, id)
			alterouReservas = true

			msg := fmt.Sprintf("Aviso: Sua reserva de bilhete #%d foi cancelada porque a carona #%d foi cancelada pelo motorista.", id, caronaID)
			InstanciaReservas.adicionarNotificacaoSemLock(res.Passageiro, msg)
			alterouNotificacoes = true
		}
	}

	if alterouReservas {
		_ = InstanciaReservas.salvarEmDisco()
	}
	if alterouNotificacoes {
		_ = InstanciaReservas.salvarNotificacoesEmDisco()
	}
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
	handlers := rest.NovoHandlersREST(Config.Empresa, InstanciaCaronas, nil)
	handlers.RegistrarRotas(mux)
	return mux
}
