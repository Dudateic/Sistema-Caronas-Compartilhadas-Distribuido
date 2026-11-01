package servicos

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/distribuido"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/rest"
)

var (
	AoCancelarCarona       func(caronaID int)
	AoCancelarCaronaRemota func(empresa string, caronaID int)
	InstanciaCaronas       *GerenciadorCaronas
	InstanciaReservas      *GerenciadorReservas
	InstanciaUsuarios      *GerenciadorUsuarios
	InstanciaParticipante  *distribuido.Participante2PC
	InstanciaCoordenador   *distribuido.Coordenador2PC
	InstanciaClienteREST   *rest.ClienteRESTInterEmpresas
)

func InicializarCaronas() error {
	var err error
	InstanciaCaronas, err = NovoGerenciadorCaronas(Config.Empresa, DiretorioDados)
	return err
}

func InicializarReservas() error {
	InstanciaReservas = NovoGerenciadorReservas(Config.Empresa, DiretorioDados)
	InstanciaUsuarios = NovoGerenciadorUsuarios(DiretorioDados)
	AoCancelarCaronaRemota = LimparReservasDeCaronaExterna
	return nil
}

func InicializarTransacoes() error {
	// Um log de transações por papel (participante e coordenador), com fsync a cada registro.
	walParticipante, err := distribuido.NovoLogTransacoes(filepath.Join(DiretorioDados, "participante.log"))
	if err != nil {
		return err
	}
	walCoordenador, err := distribuido.NovoLogTransacoes(filepath.Join(DiretorioDados, "coordenador.log"))
	if err != nil {
		return err
	}

	// Recupera os votos pendentes e reconcilia os assentos antes de aceitar tráfego.
	InstanciaParticipante, err = distribuido.NovoParticipante2PC(Config.Empresa, InstanciaCaronas, walParticipante)
	if err != nil {
		return err
	}

	InstanciaClienteREST = rest.NovoClienteREST(Config.TimeoutPar)
	InstanciaCoordenador = distribuido.NovoCoordenador2PC(
		Config.Empresa,
		"",
		InstanciaClienteREST,
		walCoordenador,
		InstanciaParticipante,
		Config.Pares,
	)

	// Participante com voto SIM sem resposta pergunta a decisão ao coordenador da transação.
	InstanciaParticipante.DefinirConsultaDecisao(func(coordenador, txID string) (string, error) {
		if strings.EqualFold(coordenador, Config.Empresa) {
			return InstanciaCoordenador.Decisao(txID), nil
		}
		url, ok := Config.Pares[coordenador]
		if !ok {
			return "", fmt.Errorf("coordenador %s desconhecido", coordenador)
		}
		return InstanciaClienteREST.ConsultarDecisao(url, txID)
	})

	// Se o coordenador estiver fora do ar, os participantes decidem entre si (terminação cooperativa).
	InstanciaParticipante.DefinirConsultaPares(func(empresa, txID string) (string, error) {
		for nome, url := range Config.Pares {
			if strings.EqualFold(nome, empresa) {
				return InstanciaClienteREST.ConsultarEstado(url, txID)
			}
		}
		return "", fmt.Errorf("empresa %s desconhecida", empresa)
	})

	// Retoma decisões que o coordenador gravou mas não chegou a entregar.
	return InstanciaCoordenador.Recuperar()
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

func LimparReservasDeCaronaExterna(empresaOrigem string, caronaID int) {
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
			if t.CaronaID == caronaID && strings.EqualFold(t.Empresa, empresaOrigem) {
				afetada = true
				break
			}
		}

		if afetada {
			delete(InstanciaReservas.reservas, id)
			alterouReservas = true

			msg := fmt.Sprintf("Aviso: Sua reserva de bilhete #%d foi cancelada porque a carona #%d da Empresa %s foi cancelada pelo motorista.", id, caronaID, empresaOrigem)
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

func NovoHandlerAPI() http.Handler {
	mux := http.NewServeMux()
	handlers := rest.NovoHandlersREST(
		Config.Empresa,
		InstanciaCaronas,
		InstanciaParticipante,
		InstanciaCoordenador,
		InstanciaReservas,
		LimparReservasDeCaronaExterna,
	)
	handlers.RegistrarRotas(mux)
	return mux
}
