package servicos

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/distribuido"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/rest"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/visual"
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
	InstanciaOutbox = NovoOutboxRemoto(DiretorioDados)
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

// LimparReservasDaCarona trata o cancelamento, nesta empresa, de uma carona desta empresa: remove as
// reservas guardadas aqui que dependiam dela, avisa os passageiros e DEVOLVE os assentos que essas
// reservas ocupavam nos demais trechos (inclusive em outras empresas).
func LimparReservasDaCarona(caronaID int) {
	desfeitas := removerReservasDaCarona(caronaID, func(t modelos.TrechoViagem) bool {
		return t.CaronaID == caronaID && (t.Empresa == Config.Empresa || t.Empresa == "")
	}, "carona #%d foi cancelada pelo motorista.", caronaID)

	for _, res := range desfeitas {
		liberarOutrosTrechos(res.Passageiro, res.Trechos, func(t modelos.TrechoViagem) bool {
			return t.CaronaID == caronaID && (t.Empresa == Config.Empresa || t.Empresa == "")
		})
	}
}

// LimparReservasDeCaronaExterna trata o aviso de que uma carona de OUTRA empresa foi cancelada.
// É idempotente: se a reserva já foi desfeita, não há nada a fazer.
func LimparReservasDeCaronaExterna(empresaOrigem string, caronaID int) {
	pertence := func(t modelos.TrechoViagem) bool {
		return t.CaronaID == caronaID && strings.EqualFold(t.Empresa, empresaOrigem)
	}
	desfeitas := removerReservasDaCarona(caronaID, pertence,
		"carona #%d da Empresa "+empresaOrigem+" foi cancelada pelo motorista.", caronaID)

	visual.MensagemAviso(fmt.Sprintf("[Cancelamento] Recebido aviso: carona #%d da empresa %s cancelada; %d reserva(s) local(is) desfeita(s)", caronaID, empresaOrigem, len(desfeitas)))
	for _, res := range desfeitas {
		liberarOutrosTrechos(res.Passageiro, res.Trechos, pertence)
	}
}

// removerReservasDaCarona apaga as reservas que contêm algum trecho da carona e notifica os passageiros.
// Não faz chamadas de rede: devolve as reservas desfeitas para o chamador liberar os demais trechos.
func removerReservasDaCarona(_ int, doTrecho func(modelos.TrechoViagem) bool, motivoFmt string, args ...any) []modelos.Reserva {
	if InstanciaReservas == nil {
		return nil
	}

	InstanciaReservas.mu.Lock()
	defer InstanciaReservas.mu.Unlock()

	var desfeitas []modelos.Reserva
	for id, res := range InstanciaReservas.reservas {
		afetada := false
		for _, t := range res.Trechos {
			if doTrecho(t) {
				afetada = true
				break
			}
		}
		if !afetada {
			continue
		}
		delete(InstanciaReservas.reservas, id)
		desfeitas = append(desfeitas, res)
		InstanciaReservas.adicionarNotificacaoSemLock(res.Passageiro,
			fmt.Sprintf("Aviso: Sua reserva de bilhete #%d foi cancelada porque a "+motivoFmt, append([]any{id}, args...)...))
	}

	if len(desfeitas) > 0 {
		_ = InstanciaReservas.salvarEmDisco()
		_ = InstanciaReservas.salvarNotificacoesEmDisco()
	}
	return desfeitas
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
