package main

import (
	"Sistema-Caronas-Compartilhadas-Distribuido/service"
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/conexao"
	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/visual"
)

type sessaoPassageiro struct {
	endereco   string
	cli        *conexao.ClienteTCP
	teclado    *bufio.Reader
	passageiro string
}

func (s *sessaoPassageiro) ler(prompt string) string {
	fmt.Print(prompt)
	linha, err := s.teclado.ReadString('\n')
	if err != nil && strings.TrimSpace(linha) == "" {
		if errors.Is(err, io.EOF) {
			fmt.Println("\nEntrada encerrada. Ate logo!")
		}
		_ = s.cli.Fechar()
		os.Exit(0)
	}
	return strings.TrimSpace(linha)
}

func LimparTela() {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "cls")
	} else {
		cmd = exec.Command("clear")
	}
	cmd.Stdout = os.Stdout
	_ = cmd.Run()
}

func (s *sessaoPassageiro) reconectar() error {
	if s.cli != nil {
		_ = s.cli.Fechar()
	}
	var ultimo error
	for i := 1; i <= 3; i++ {
		fmt.Printf("[REDE] Reconectando a %s (tentativa %d/%d)...\n", s.endereco, i, 3)
		cli, err := conexao.ConectarTCP(s.endereco, 2*time.Second)
		if err == nil {
			s.cli = cli
			fmt.Println("[REDE] Conexao restabelecida com sucesso.")
			return nil
		}
		ultimo = err
		time.Sleep(time.Second)
	}
	return fmt.Errorf("servidor indisponivel: %w", ultimo)
}

func (s *sessaoPassageiro) garantirConexao() error {
	select {
	case err := <-s.cli.Erros:
		fmt.Printf("[REDE] Conexao perdida detectada (%v).\n", err)
		return s.reconectar()
	default:
		return nil
	}
}

func (s *sessaoPassageiro) trocar(req any, tipoEsperado string, destino any) error {
	if err := s.garantirConexao(); err != nil {
		return err
	}

	// Drena mensagens residuais que possam ter sobrado no buffer do canal
	for drenando := true; drenando; {
		select {
		case <-s.cli.MensagensRecebidas:
		default:
			drenando = false
		}
	}

	if err := s.cli.EnviarJSON(req); err != nil {
		return s.falhaDeConexao(err)
	}

	limite := time.NewTimer(10 * time.Second)
	defer limite.Stop()

	for {
		select {
		case linha := <-s.cli.MensagensRecebidas:
			var base protocolo.MensagemBase
			if err := json.Unmarshal(linha, &base); err != nil || base.Tipo != tipoEsperado {
				continue
			}
			if err := json.Unmarshal(linha, destino); err != nil {
				return fmt.Errorf("resposta invalida do servidor: %w", err)
			}
			return nil

		case err := <-s.cli.Erros:
			return s.falhaDeConexao(err)

		case <-limite.C:
			return s.falhaDeConexao(errors.New("tempo limite esgotado aguardando resposta do servidor"))
		}
	}
}

func (s *sessaoPassageiro) falhaDeConexao(causa error) error {
	fmt.Printf("\n[AVISO] %v\n", causa)
	if err := s.reconectar(); err != nil {
		return err
	}
	return errors.New("a comunicacao falhou; confira o status da operacao em 'Minhas Reservas'")
}

func main() {
	padrao := os.Getenv("SERVIDOR")
	if padrao == "" {
		padrao = conexao.EnderecoPadrao
	}
	endereco := flag.String("servidor", padrao, "servidor da sua empresa (host:porta)")
	flag.Parse()

	visual.ExibirCabecalho("VAIJUNTO - MODULO DO PASSAGEIRO")
	fmt.Printf("Conectando ao servidor em %s...\n", *endereco)

	cli, err := conexao.ConectarTCP(*endereco, 5*time.Second)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao conectar: %v", err))
		os.Exit(1)
	}

	s := &sessaoPassageiro{
		endereco: *endereco,
		cli:      cli,
		teclado:  bufio.NewReader(os.Stdin),
	}
	defer func() { _ = s.cli.Fechar() }()

	visual.MensagemSucesso("Conexao estabelecida com sucesso!")
	time.Sleep(500 * time.Millisecond)

	if !s.telaAcesso() {
		fmt.Println("Operacao encerrada pelo usuario. Ate logo!")
		return
	}

	s.menuPrincipal()
}

func (s *sessaoPassageiro) telaAcesso() bool {
	for {
		LimparTela()
		visual.ExibirCabecalho("AUTENTICACAO DO PASSAGEIRO")
		fmt.Println("1. Entrar (Login)")
		fmt.Println("2. Criar Nova Conta (Cadastro)")
		fmt.Println("0. Encerrar")
		fmt.Println()

		opcao := s.ler("Escolha uma opcao: ")

		switch opcao {
		case "1":
			usuario := s.ler("Usuario: ")
			senha := s.ler("Senha: ")

			if usuario == "" || senha == "" {
				visual.MensagemAviso("Usuario e senha nao podem ser vazios.")
				s.ler("\nPressione ENTER para continuar...")
				continue
			}

			fmt.Println("Validando credenciais...")
			sucesso, err := service.AutenticarCliente(s.cli, usuario, senha, protocolo.PerfilPassageiro)
			if err != nil {
				visual.MensagemErro(fmt.Sprintf("Falha na autenticacao: %v", err))
				s.ler("\nPressione ENTER para continuar...")
				continue
			}
			if sucesso {
				s.passageiro = usuario
				return true
			}
			visual.MensagemAviso("Credenciais invalidas ou perfil incompativel.")
			s.ler("\nPressione ENTER para continuar...")

		case "2":
			usuario := s.ler("Defina seu nome de usuario: ")
			senha := s.ler("Defina sua senha: ")

			if usuario == "" || senha == "" {
				visual.MensagemAviso("Preencha usuario e senha para se cadastrar.")
				s.ler("\nPressione ENTER para continuar...")
				continue
			}

			fmt.Println("Enviando solicitacao de cadastro...")
			sucesso, err := service.CadastrarCliente(s.cli, usuario, senha, protocolo.PerfilPassageiro)
			if err != nil {
				visual.MensagemErro(fmt.Sprintf("Falha ao registrar usuario: %v", err))
				s.ler("\nPressione ENTER para continuar...")
				continue
			}
			if sucesso {
				visual.MensagemSucesso("Conta criada com sucesso! Realize login na opcao 1.")
				s.ler("\nPressione ENTER para continuar...")
			}

		case "0":
			return false

		default:
			visual.MensagemAviso("Opcao invalida, tente novamente.")
			s.ler("\nPressione ENTER para continuar...")
		}
	}
}

func (s *sessaoPassageiro) menuPrincipal() {
	s.consultarNotificacoesSilenciosa()

	for {
		LimparTela()
		visual.ExibirCabecalho(fmt.Sprintf("PAINEL DO PASSAGEIRO: %s", s.passageiro))
		fmt.Println("1. Buscar Itinerarios e Reservar")
		fmt.Println("2. Consultar Minhas Reservas")
		fmt.Println("3. Cancelar Reserva")
		fmt.Println("4. Ver Notificacoes de Viagem")
		fmt.Println("0. Sair e Desconectar")
		fmt.Println()

		opcao := s.ler("Selecione a opcao desejada: ")

		switch opcao {
		case "1":
			s.buscarEReservar()
		case "2":
			s.consultarReservas()
		case "3":
			s.cancelarReserva()
		case "4":
			s.consultarNotificacoes()
		case "0":
			fmt.Println("Desconectando do servidor... Ate logo!")
			return
		default:
			visual.MensagemAviso("Opcao invalida. Digite um numero entre 0 e 4.")
			s.ler("\nPressione ENTER para continuar...")
		}
	}
}

func (s *sessaoPassageiro) buscarEReservar() {
	LimparTela()
	visual.ExibirCabecalho("BUSCAR ITINERARIOS")

	origem := s.ler("Cidade de partida (Origem): ")
	destino := s.ler("Cidade de chegada (Destino): ")
	dataTxt := s.ler("Data da viagem (AAAA-MM-DD): ")

	if origem == "" || destino == "" || dataTxt == "" {
		visual.MensagemErro("Origem, destino e data sao campos obrigatorios.")
		s.ler("\nPressione ENTER para voltar...")
		return
	}

	fmt.Println("\nBuscando rotas disponiveis no servidor e no consorcio...")
	var respBusca protocolo.RespostaBuscarItinerarios
	err := s.trocar(protocolo.ConsultaItinerario{
		Tipo:    protocolo.TipoBuscarItinerariosReq,
		Origem:  origem,
		Destino: destino,
		Data:    dataTxt,
	}, protocolo.TipoBuscarItinerariosRes, &respBusca)

	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha na busca: %v", err))
		s.ler("\nPressione ENTER para voltar...")
		return
	}

	if !respBusca.Sucesso || len(respBusca.Itinerarios) == 0 {
		visual.MensagemAviso("Nenhum itinerario disponivel para essa rota e data.")
		s.ler("\nPressione ENTER para voltar...")
		return
	}

	LimparTela()
	visual.ExibirCabecalho("ITINERARIOS ENCONTRADOS")

	for i, it := range respBusca.Itinerarios {
		fmt.Printf("\n[Opcao %d] - Preco Total: R$ %.2f\n", i+1, it.PrecoTotal)
		trechoCols := []string{"Empresa", "Origem -> Destino", "Horario", "Preco"}
		trechoLarguras := []int{15, 30, 8, 10}
		visual.TabelaCabecalho(trechoCols, trechoLarguras)
		for _, t := range it.Trechos {
			rotaStr := fmt.Sprintf("%s -> %s", t.Origem, t.Destino)
			precoStr := fmt.Sprintf("R$ %.2f", t.Preco)
			visual.TabelaLinha([]string{t.Empresa, rotaStr, t.Horario, precoStr}, trechoLarguras)
		}
		visual.TabelaRodape(trechoLarguras)
	}

	fmt.Println()
	escolhaStr := s.ler("Deseja reservar alguma dessas opcoes? Digite o numero (ou 0 para cancelar): ")
	escolha, err := strconv.Atoi(escolhaStr)
	if err != nil || escolha <= 0 || escolha > len(respBusca.Itinerarios) {
		fmt.Println("Nenhuma reserva efetuada.")
		s.ler("\nPressione ENTER para voltar...")
		return
	}

	itinerarioEscolhido := respBusca.Itinerarios[escolha-1]
	fmt.Printf("\nProcessando transacao atomica de reserva para %s...\n", s.passageiro)

	var respReserva protocolo.RespostaReservar
	err = s.trocar(protocolo.SolicitacaoReserva{
		Tipo:       protocolo.TipoReservarItinerarioReq,
		Passageiro: s.passageiro,
		Data:       itinerarioEscolhido.Data,
		Trechos:    itinerarioEscolhido.Trechos,
	}, protocolo.TipoReservarItinerarioRes, &respReserva)

	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao processar reserva: %v", err))
		s.ler("\nPressione ENTER para voltar...")
		return
	}

	if respReserva.Sucesso {
		visual.MensagemSucesso(fmt.Sprintf("%s Codigo da Reserva: #%d", respReserva.Mensagem, respReserva.ReservaID))
	} else {
		visual.MensagemAviso(fmt.Sprintf("Nao foi possivel concluir a reserva: %s", respReserva.Mensagem))
	}
	s.ler("\nPressione ENTER para voltar ao menu...")
}

func (s *sessaoPassageiro) buscarMinhasReservas() ([]protocolo.Reserva, error) {
	var resp protocolo.RespostaConsultarReservas
	err := s.trocar(protocolo.ConsultaReservasPassageiro{
		Tipo:       protocolo.TipoConsultarReservasReq,
		Passageiro: s.passageiro,
	}, protocolo.TipoConsultarReservasRes, &resp)

	if err != nil {
		return nil, err
	}
	if !resp.Sucesso {
		return nil, errors.New(resp.Mensagem)
	}
	return resp.Reservas, nil
}

func (s *sessaoPassageiro) consultarReservas() {
	LimparTela()
	visual.ExibirCabecalho("SUAS RESERVAS ATIVAS")

	listaReservas, err := s.buscarMinhasReservas()
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao consultar reservas: %v", err))
		s.ler("\nPressione ENTER para continuar...")
		return
	}

	if len(listaReservas) == 0 {
		visual.MensagemAviso("Voce nao possui nenhuma reserva ativa.")
		s.ler("\nPressione ENTER para continuar...")
		return
	}

	colunasCabecalho := []string{"ID", "Data da Viagem", "Preco Total"}
	largurasColunas := []int{6, 20, 15}

	visual.TabelaCabecalho(colunasCabecalho, largurasColunas)
	for _, r := range listaReservas {
		visual.TabelaLinha([]string{
			strconv.Itoa(r.ID),
			r.Data,
			fmt.Sprintf("R$ %.2f", r.PrecoTotal),
		}, largurasColunas)
	}
	visual.TabelaRodape(largurasColunas)

	fmt.Println()
	visual.LinhaDivisoria()
	fmt.Println("DETALHES DOS TRECHOS:")
	for _, r := range listaReservas {
		fmt.Printf("\n[Reserva #%d]\n", r.ID)
		if len(r.Trechos) > 0 {
			trechoCols := []string{"Empresa", "Origem -> Destino", "Horario", "Preco"}
			trechoLarguras := []int{15, 30, 8, 10}
			visual.TabelaCabecalho(trechoCols, trechoLarguras)
			for _, t := range r.Trechos {
				trechoStr := fmt.Sprintf("%s -> %s", t.Origem, t.Destino)
				precoStr := fmt.Sprintf("R$ %.2f", t.Preco)
				visual.TabelaLinha([]string{t.Empresa, trechoStr, t.Horario, precoStr}, trechoLarguras)
			}
			visual.TabelaRodape(trechoLarguras)
		}
	}

	s.ler("\nPressione ENTER para voltar ao menu...")
}

func (s *sessaoPassageiro) cancelarReserva() {
	LimparTela()
	visual.ExibirCabecalho("CANCELAR RESERVA")

	listaReservas, err := s.buscarMinhasReservas()
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao consultar reservas: %v", err))
		s.ler("\nPressione ENTER para continuar...")
		return
	}

	if len(listaReservas) == 0 {
		visual.MensagemAviso("Voce nao possui nenhuma reserva para cancelar.")
		s.ler("\nPressione ENTER para continuar...")
		return
	}

	colunasCabecalho := []string{"ID", "Data", "Rota", "Preco Total"}
	largurasColunas := []int{4, 12, 30, 12}
	visual.TabelaCabecalho(colunasCabecalho, largurasColunas)
	for _, r := range listaReservas {
		origem, destino := "Indefinido", "Indefinido"
		if len(r.Trechos) > 0 {
			origem = r.Trechos[0].Origem
			destino = r.Trechos[len(r.Trechos)-1].Destino
		}
		rotaStr := fmt.Sprintf("%s -> %s", origem, destino)
		visual.TabelaLinha([]string{
			strconv.Itoa(r.ID),
			r.Data,
			rotaStr,
			fmt.Sprintf("R$ %.2f", r.PrecoTotal),
		}, largurasColunas)
	}
	visual.TabelaRodape(largurasColunas)
	fmt.Println()

	idStr := s.ler("Informe o ID da reserva que deseja cancelar (ou 0 para voltar): ")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		return
	}

	confirmacao := s.ler(fmt.Sprintf("Tem certeza que deseja cancelar a reserva #%d? (s/n): ", id))
	if strings.ToLower(confirmacao) != "s" && strings.ToLower(confirmacao) != "sim" {
		fmt.Println("Cancelamento abortado.")
		s.ler("\nPressione ENTER para voltar...")
		return
	}

	var resp protocolo.RespostaSimples
	err = s.trocar(protocolo.CancelamentoReserva{
		Tipo:       protocolo.TipoCancelarReservaReq,
		ReservaID:  id,
		Passageiro: s.passageiro,
	}, protocolo.TipoCancelarReservaRes, &resp)

	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao comunicar cancelamento: %v", err))
		s.ler("\nPressione ENTER para voltar...")
		return
	}

	if resp.Sucesso {
		visual.MensagemSucesso(resp.Mensagem)
	} else {
		visual.MensagemAviso(resp.Mensagem)
	}
	s.ler("\nPressione ENTER para voltar ao menu...")
}

func (s *sessaoPassageiro) consultarNotificacoes() {
	LimparTela()
	visual.ExibirCabecalho("CAIXA DE MENSAGENS")

	var resp protocolo.RespostaConsultarNotificacoes
	err := s.trocar(protocolo.ConsultaNotificacoes{
		Tipo:       protocolo.TipoConsultarNotificacoesReq,
		Passageiro: s.passageiro,
	}, protocolo.TipoConsultarNotificacoesRes, &resp)

	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao ler notificacoes: %v", err))
		s.ler("\nPressione ENTER para continuar...")
		return
	}

	if len(resp.Notificacoes) == 0 {
		visual.MensagemAviso("Voce nao possui novas notificacoes.")
		s.ler("\nPressione ENTER para continuar...")
		return
	}

	for _, n := range resp.Notificacoes {
		fmt.Printf("\n[Recebido em: %s]\n-> %s\n", n.Data, n.Mensagem)
		visual.LinhaDivisoria()
	}

	fmt.Println("\n(Avisos lidos)")
	s.ler("\nPressione ENTER para voltar ao menu...")
}

func (s *sessaoPassageiro) consultarNotificacoesSilenciosa() {
	var resp protocolo.RespostaConsultarNotificacoes
	err := s.trocar(protocolo.ConsultaNotificacoes{
		Tipo:       protocolo.TipoConsultarNotificacoesReq,
		Passageiro: s.passageiro,
	}, protocolo.TipoConsultarNotificacoesRes, &resp)

	if err == nil && len(resp.Notificacoes) > 0 {
		LimparTela()
		visual.ExibirCabecalho("NOVAS NOTIFICACOES RECEBIDAS")
		for _, n := range resp.Notificacoes {
			fmt.Printf("\n[Recebido em: %s]\n-> %s\n", n.Data, n.Mensagem)
		}
		visual.LinhaDivisoria()
		s.ler("\nPressione ENTER para ir para o Painel Principal...")
	}
}
