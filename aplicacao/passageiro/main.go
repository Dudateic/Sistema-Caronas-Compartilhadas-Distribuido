package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/visual"
)

const (
	tentativasReconexao = 3
	intervaloReconexao  = time.Second
	timeoutResposta     = 10 * time.Second
)

var scanner = bufio.NewScanner(os.Stdin)

func lerEntrada(prompt string) string {
	fmt.Print(visual.TextoBranco + prompt + visual.Reset)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

// LimparTela identifica o SO e limpa o terminal para manter a navegação limpa
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

type sessaoPassageiro struct {
	endereco string
	cli      *cliente_servidor.ClienteTCP
	usuario  string
}

func (s *sessaoPassageiro) confirmar(pergunta string) bool {
	r := strings.ToLower(lerEntrada(pergunta + " (s/n): "))
	return r == "s" || r == "sim"
}

func (s *sessaoPassageiro) reconectar() error {
	if s.cli != nil {
		_ = s.cli.Fechar()
	}
	for i := 1; i <= tentativasReconexao; i++ {
		visual.MensagemAviso(fmt.Sprintf("Reconectando a %s (tentativa %d/%d)...", s.endereco, i, tentativasReconexao))
		cli, err := cliente_servidor.ConectarTCP(s.endereco, 2*time.Second)
		if err == nil {
			s.cli = cli
			visual.MensagemSucesso("Conexão restabelecida com sucesso.")
			return nil
		}
		time.Sleep(intervaloReconexao)
	}
	return fmt.Errorf("servidor indisponível em %s", s.endereco)
}

func (s *sessaoPassageiro) garantirConexao() error {
	select {
	case err := <-s.cli.Erros:
		visual.MensagemErro(fmt.Sprintf("Conexão interrompida (%v).", err))
		return s.reconectar()
	default:
		return nil
	}
}

func (s *sessaoPassageiro) trocar(req any, tipoEsperado string, destino any) error {
	if err := s.garantirConexao(); err != nil {
		return err
	}

	// Drena mensagens residuais pendentes no buffer
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

	limite := time.NewTimer(timeoutResposta)
	defer limite.Stop()

	for {
		select {
		case linha := <-s.cli.MensagensRecebidas:
			var base struct {
				Tipo string `json:"tipo"`
			}
			if err := json.Unmarshal(linha, &base); err != nil || base.Tipo != tipoEsperado {
				continue
			}
			if err := json.Unmarshal(linha, destino); err != nil {
				return fmt.Errorf("resposta inválida do servidor: %w", err)
			}
			return nil

		case err := <-s.cli.Erros:
			return s.falhaDeConexao(err)

		case <-limite.C:
			return s.falhaDeConexao(errors.New("tempo limite esgotado aguardando o servidor"))
		}
	}
}

func (s *sessaoPassageiro) falhaDeConexao(causa error) error {
	visual.MensagemErro(fmt.Sprintf("%v", causa))
	if err := s.reconectar(); err != nil {
		return err
	}
	return errors.New("a resposta se perdeu durante a transmissão; verifique suas reservas ativas")
}

func main() {
	LimparTela()
	visual.ExibirCabecalho("VAIJUNTO - MODULO DO PASSAGEIRO")

	padrao := os.Getenv("SERVIDOR")
	if padrao == "" {
		padrao = "localhost:8081"
	}

	flagEndereco := flag.String("servidor", "", "servidor da sua empresa (host:porta)")
	flag.Parse()

	endereco := *flagEndereco
	if endereco == "" {
		fmt.Printf("Servidor predefinido configurado: %s\n", padrao)
		entrada := lerEntrada("Pressione ENTER para conectar ou digite outro IP:Porta: ")
		if entrada != "" {
			endereco = entrada
		} else {
			endereco = padrao
		}
	}

	fmt.Printf("\nA tentar conectar ao servidor em %s...\n", endereco)
	cli, err := cliente_servidor.ConectarTCP(endereco, 5*time.Second)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao conectar a %s: %v", endereco, err))
		lerEntrada("\nPressione ENTER para sair...")
		return
	}
	defer func() { _ = cli.Fechar() }()

	visual.MensagemSucesso(fmt.Sprintf("Conectado com sucesso ao servidor em %s!", endereco))
	time.Sleep(1500 * time.Millisecond)

	s := &sessaoPassageiro{
		endereco: endereco,
		cli:      cli,
	}

	if !s.telaAcesso() {
		fmt.Println("Operacao encerrada pelo utilizador. Ate logo!")
		return
	}

	s.menuPrincipalPassageiro()
}

func (s *sessaoPassageiro) telaAcesso() bool {
	for {
		LimparTela()
		visual.ExibirCabecalho("AUTENTICACAO DO PASSAGEIRO")
		fmt.Println("1. Entrar (Login)")
		fmt.Println("2. Criar Nova Conta (Cadastro)")
		fmt.Println("0. Encerrar")
		fmt.Println()

		opcao := lerEntrada("Escolha uma opcao: ")

		switch opcao {
		case "1":
			usuario := lerEntrada("Usuario: ")
			senha := lerEntrada("Senha: ")

			if usuario == "" || senha == "" {
				visual.MensagemAviso("Usuario e senha nao podem ser vazios.")
				lerEntrada("\nPressione ENTER para continuar...")
				continue
			}

			req := map[string]any{
				"tipo":  cliente_servidor.TipoLoginReq,
				"login": usuario,
				"senha": senha,
			}

			var resp cliente_servidor.RespostaLogin
			err := s.trocar(req, cliente_servidor.TipoLoginRes, &resp)
			if err != nil {
				visual.MensagemErro(fmt.Sprintf("Falha na comunicacao com servidor: %v", err))
				lerEntrada("\nPressione ENTER para continuar...")
				continue
			}

			if !resp.Sucesso {
				visual.MensagemAviso(fmt.Sprintf("Falha no login: %s", resp.Mensagem))
				lerEntrada("\nPressione ENTER para continuar...")
				continue
			}

			if resp.TipoUsuario != string(modelos.UsuarioPassageiro) {
				visual.MensagemErro(fmt.Sprintf("Acesso negado: esta conta e de '%s'. Use o modulo de motorista.", resp.TipoUsuario))
				lerEntrada("\nPressione ENTER para continuar...")
				continue
			}

			s.usuario = usuario
			visual.MensagemSucesso(fmt.Sprintf("Bem-vindo(a), %s!", s.usuario))
			time.Sleep(1 * time.Second)
			return true

		case "2":
			usuario := lerEntrada("Defina seu nome de usuario: ")
			senha := lerEntrada("Defina sua senha: ")

			if usuario == "" || senha == "" {
				visual.MensagemAviso("Preencha usuario e senha para se cadastrar.")
				lerEntrada("\nPressione ENTER para continuar...")
				continue
			}

			req := map[string]any{
				"tipo":         cliente_servidor.TipoCadastroReq,
				"nome":         usuario,
				"login":        usuario,
				"senha":        senha,
				"tipo_usuario": string(modelos.UsuarioPassageiro),
			}

			var resp cliente_servidor.RespostaSimples
			err := s.trocar(req, cliente_servidor.TipoCadastroRes, &resp)
			if err != nil {
				visual.MensagemErro(fmt.Sprintf("Falha ao registrar usuario: %v", err))
				lerEntrada("\nPressione ENTER para continuar...")
				continue
			}

			if resp.Sucesso {
				visual.MensagemSucesso("Conta de passageiro criada! Faca login na opcao 1.")
			} else {
				visual.MensagemErro(resp.Mensagem)
			}
			lerEntrada("\nPressione ENTER para continuar...")

		case "0":
			return false

		default:
			visual.MensagemAviso("Opcao invalida, tente novamente.")
			lerEntrada("\nPressione ENTER para continuar...")
		}
	}
}

func (s *sessaoPassageiro) menuPrincipalPassageiro() {
	// Notificações silenciosas ao logar
	s.verificarNotificacoesSilenciosa()

	for {
		LimparTela()
		visual.ExibirCabecalho(fmt.Sprintf("PAINEL DO PASSAGEIRO: %s", s.usuario))
		fmt.Println("1. Buscar Itinerarios no Consorcio e Reservar (2PC)")
		fmt.Println("2. Consultar Minhas Reservas")
		fmt.Println("3. Cancelar Reserva")
		fmt.Println("4. Ver Notificacoes de Viagem")
		fmt.Println("0. Sair e Desconectar")
		fmt.Println()

		opcao := lerEntrada("Selecione a opcao desejada: ")

		switch opcao {
		case "1":
			s.acaoBuscarEReservar()
		case "2":
			s.acaoConsultarReservas()
		case "3":
			s.acaoCancelarReserva()
		case "4":
			s.acaoConsultarNotificacoes()
		case "0":
			fmt.Println("Desconectando do servidor... Ate logo!")
			return
		default:
			visual.MensagemAviso("Opcao invalida. Digite um numero entre 0 e 4.")
			lerEntrada("\nPressione ENTER para continuar...")
		}
	}
}

func (s *sessaoPassageiro) acaoBuscarEReservar() {
	LimparTela()
	visual.ExibirCabecalho("BUSCAR ITINERARIOS NO CONSORCIO")

	origem := lerEntrada("Cidade de partida (Origem): ")
	destino := lerEntrada("Cidade de chegada (Destino): ")

	if origem == "" || destino == "" {
		visual.MensagemErro("Origem e destino sao campos obrigatorios.")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	fmt.Println("\nConsultando empresas do consorcio via API REST...")
	req := map[string]any{
		"tipo":    cliente_servidor.TipoBuscarItinerariosReq,
		"origem":  origem,
		"destino": destino,
	}

	var res cliente_servidor.RespostaBuscarItinerarios
	err := s.trocar(req, cliente_servidor.TipoBuscarItinerariosRes, &res)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha na busca: %v", err))
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	if len(res.Itinerarios) == 0 {
		visual.MensagemAviso("Nenhum itinerario disponivel para essa rota no consorcio.")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	LimparTela()
	visual.ExibirCabecalho("ITINERARIOS ENCONTRADOS")

	colunas := []string{"Opcao", "Preco Total", "Qtd Trechos", "Composicao da Rota (Empresa / Trajeto)"}
	larguras := []int{7, 13, 11, 48}
	visual.TabelaCabecalho(colunas, larguras)

	for i, itin := range res.Itinerarios {
		var descTrechos []string
		for _, tr := range itin.Trechos {
			descTrechos = append(descTrechos, fmt.Sprintf("[%s] %s->%s (R$ %.2f)", tr.Empresa, tr.Origem, tr.Destino, tr.Preco))
		}
		rotaCompleta := strings.Join(descTrechos, " + ")
		visual.TabelaLinha([]string{
			fmt.Sprintf("#%d", i+1),
			fmt.Sprintf("R$ %.2f", itin.PrecoTotal),
			strconv.Itoa(len(itin.Trechos)),
			rotaCompleta,
		}, larguras)
	}
	visual.TabelaRodape(larguras)

	fmt.Println()
	escolhaStr := lerEntrada("Deseja reservar alguma dessas opcoes? Digite o numero da opcao (ou 0 para cancelar): ")
	escolha, err := strconv.Atoi(escolhaStr)
	if err != nil || escolha <= 0 || escolha > len(res.Itinerarios) {
		fmt.Println("Nenhuma reserva efetuada.")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	itinerarioEscolhido := res.Itinerarios[escolha-1]

	if !s.confirmar(fmt.Sprintf("Confirmar reserva atomica (2PC) da opcao %d no valor de R$ %.2f?", escolha, itinerarioEscolhido.PrecoTotal)) {
		visual.MensagemAviso("Reserva cancelada pelo passageiro.")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	visual.MensagemAviso("Disparando Two-Phase Commit com os servidores das empresas...")

	reqReserva := map[string]any{
		"tipo":       cliente_servidor.TipoReservarItinerarioReq,
		"passageiro": s.usuario,
		"itinerario": itinerarioEscolhido,
	}

	var respRes cliente_servidor.RespostaReservar
	err = s.trocar(reqReserva, cliente_servidor.TipoReservarItinerarioRes, &respRes)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao comunicar com o coordenador 2PC: %v", err))
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	if respRes.Sucesso {
		visual.MensagemSucesso(fmt.Sprintf("Reserva confirmada em todo o consorcio! Codigo do Bilhete: #%d", respRes.ReservaID))
	} else {
		visual.MensagemErro(fmt.Sprintf("Nao foi possivel reservar (GLOBAL ABORT): %s", respRes.Mensagem))
	}
	lerEntrada("\nPressione ENTER para voltar ao menu...")
}

func (s *sessaoPassageiro) acaoConsultarReservas() {
	LimparTela()
	visual.ExibirCabecalho("SUAS RESERVAS ATIVAS")

	req := map[string]any{
		"tipo":       cliente_servidor.TipoConsultarReservasReq,
		"passageiro": s.usuario,
	}

	var res struct {
		Sucesso  bool              `json:"sucesso"`
		Mensagem string            `json:"mensagem"`
		Reservas []modelos.Reserva `json:"reservas"`
	}

	err := s.trocar(req, cliente_servidor.TipoConsultarReservasRes, &res)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao consultar reservas: %v", err))
		lerEntrada("\nPressione ENTER para continuar...")
		return
	}

	if len(res.Reservas) == 0 {
		visual.MensagemAviso("Voce nao possui nenhuma reserva ativa.")
		lerEntrada("\nPressione ENTER para continuar...")
		return
	}

	colunasCabecalho := []string{"Bilhete", "TxID (2PC)", "Data", "Preco Total"}
	largurasColunas := []int{9, 24, 12, 14}
	visual.TabelaCabecalho(colunasCabecalho, largurasColunas)

	for _, r := range res.Reservas {
		visual.TabelaLinha([]string{
			fmt.Sprintf("#%d", r.ID),
			r.TxID,
			r.Data,
			fmt.Sprintf("R$ %.2f", r.PrecoTotal),
		}, largurasColunas)
	}
	visual.TabelaRodape(largurasColunas)

	fmt.Println()
	visual.LinhaDivisoria()
	fmt.Println("DETALHES DOS TRECHOS POR BILHETE:")
	for _, r := range res.Reservas {
		fmt.Printf("\n[Bilhete #%d | Transacao 2PC: %s]\n", r.ID, r.TxID)
		if len(r.Trechos) > 0 {
			trechoCols := []string{"Empresa", "Trecho (Origem -> Destino)", "Motorista", "Preco"}
			trechoLarguras := []int{10, 30, 16, 12}
			visual.TabelaCabecalho(trechoCols, trechoLarguras)
			for _, t := range r.Trechos {
				trechoStr := fmt.Sprintf("%s -> %s", t.Origem, t.Destino)
				precoStr := fmt.Sprintf("R$ %.2f", t.Preco)
				visual.TabelaLinha([]string{t.Empresa, trechoStr, t.Motorista, precoStr}, trechoLarguras)
			}
			visual.TabelaRodape(trechoLarguras)
		}
	}

	lerEntrada("\nPressione ENTER para voltar ao menu...")
}

func (s *sessaoPassageiro) acaoCancelarReserva() {
	LimparTela()
	visual.ExibirCabecalho("CANCELAR RESERVA")

	req := map[string]any{
		"tipo":       cliente_servidor.TipoConsultarReservasReq,
		"passageiro": s.usuario,
	}

	var res struct {
		Sucesso  bool              `json:"sucesso"`
		Reservas []modelos.Reserva `json:"reservas"`
	}

	err := s.trocar(req, cliente_servidor.TipoConsultarReservasRes, &res)
	if err != nil || len(res.Reservas) == 0 {
		visual.MensagemAviso("Voce nao possui nenhuma reserva para cancelar.")
		lerEntrada("\nPressione ENTER para continuar...")
		return
	}

	colunasCabecalho := []string{"ID", "TxID", "Preco Total", "Trechos"}
	largurasColunas := []int{6, 22, 14, 25}
	visual.TabelaCabecalho(colunasCabecalho, largurasColunas)
	for _, r := range res.Reservas {
		origem := r.Trechos[0].Origem
		destino := r.Trechos[len(r.Trechos)-1].Destino
		visual.TabelaLinha([]string{
			strconv.Itoa(r.ID),
			r.TxID,
			fmt.Sprintf("R$ %.2f", r.PrecoTotal),
			fmt.Sprintf("%s -> %s", origem, destino),
		}, largurasColunas)
	}
	visual.TabelaRodape(largurasColunas)

	idStr := lerEntrada("\nInforme o ID da reserva que deseja cancelar (ou 0 para voltar): ")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		return
	}

	if !s.confirmar(fmt.Sprintf("Tem certeza que deseja cancelar a reserva #%d?", id)) {
		visual.MensagemAviso("Cancelamento abortado.")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	reqCanc := map[string]any{
		"tipo":       cliente_servidor.TipoCancelarReservaReq,
		"reserva_id": id,
		"passageiro": s.usuario,
	}

	var respCanc cliente_servidor.RespostaSimples
	err = s.trocar(reqCanc, cliente_servidor.TipoCancelarReservaRes, &respCanc)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao comunicar cancelamento: %v", err))
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	if respCanc.Sucesso {
		visual.MensagemSucesso(fmt.Sprintf("Reserva #%d cancelada com sucesso e assentos liberados.", id))
	} else {
		visual.MensagemErro(respCanc.Mensagem)
	}
	lerEntrada("\nPressione ENTER para voltar ao menu...")
}

func (s *sessaoPassageiro) acaoConsultarNotificacoes() {
	LimparTela()
	visual.ExibirCabecalho("CAIXA DE MENSAGENS")

	req := map[string]any{
		"tipo":       cliente_servidor.TipoConsultarNotificacoesReq,
		"passageiro": s.usuario,
	}

	var res cliente_servidor.RespostaConsultarNotificacoes
	err := s.trocar(req, cliente_servidor.TipoConsultarNotificacoesRes, &res)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao consultar mensagens: %v", err))
		lerEntrada("\nPressione ENTER para continuar...")
		return
	}

	if len(res.Notificacoes) == 0 {
		visual.MensagemAviso("Voce nao possui novas notificacoes.")
		lerEntrada("\nPressione ENTER para continuar...")
		return
	}

	for _, n := range res.Notificacoes {
		fmt.Printf("\n[Recebido em: %s]\n-> %s\n", n.Data, n.Mensagem)
		visual.LinhaDivisoria()
	}

	fmt.Println("\n(Avisos marcados como lidos e apagados da caixa)")
	lerEntrada("\nPressione ENTER para voltar ao menu...")
}

func (s *sessaoPassageiro) verificarNotificacoesSilenciosa() {
	req := map[string]any{
		"tipo":       cliente_servidor.TipoConsultarNotificacoesReq,
		"passageiro": s.usuario,
	}

	var res cliente_servidor.RespostaConsultarNotificacoes
	err := s.trocar(req, cliente_servidor.TipoConsultarNotificacoesRes, &res)
	if err != nil || len(res.Notificacoes) == 0 {
		return
	}

	LimparTela()
	visual.ExibirCabecalho("NOVAS NOTIFICACOES DE VIAGEM")
	for _, n := range res.Notificacoes {
		fmt.Printf("\n[Recebido em: %s]\n-> %s\n", n.Data, n.Mensagem)
	}
	visual.LinhaDivisoria()
	lerEntrada("\nPressione ENTER para ir para o Painel Principal...")
}
