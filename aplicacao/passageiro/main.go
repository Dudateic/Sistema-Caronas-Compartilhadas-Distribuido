package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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

type sessaoPassageiro struct {
	endereco string
	cli      *cliente_servidor.ClienteTCP
	teclado  *bufio.Reader
	usuario  string
}

func (s *sessaoPassageiro) ler(prompt string) string {
	fmt.Print(visual.TextoBranco + prompt + visual.Reset)
	linha, err := s.teclado.ReadString('\n')
	if err != nil && strings.TrimSpace(linha) == "" {
		if errors.Is(err, io.EOF) {
			visual.MensagemAviso("Entrada encerrada. Até logo!")
		}
		if s.cli != nil {
			_ = s.cli.Fechar()
		}
		os.Exit(0)
	}
	return strings.TrimSpace(linha)
}

func (s *sessaoPassageiro) confirmar(pergunta string) bool {
	r := strings.ToLower(s.ler(pergunta + " (s/n): "))
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

	// Drena mensagens residuais que possam ter sobrado no canal
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
	visual.ExibirCabecalho("VaiJunto - Terminal Passageiro")

	padrao := os.Getenv("SERVIDOR")
	if padrao == "" {
		padrao = "localhost:8081"
	}
	endereco := flag.String("servidor", padrao, "servidor da sua empresa (host:porta)")
	flag.Parse()

	cli, err := cliente_servidor.ConectarTCP(*endereco, 5*time.Second)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao conectar ao servidor em %s: %v", *endereco, err))
		os.Exit(1)
	}
	s := &sessaoPassageiro{endereco: *endereco, cli: cli, teclado: bufio.NewReader(os.Stdin)}
	defer func() { _ = s.cli.Fechar() }()

	visual.MensagemSucesso(fmt.Sprintf("Conectado ao servidor %s com sucesso!", *endereco))

	if !s.telaInicial() {
		return
	}
	s.menuPrincipal()
}

func (s *sessaoPassageiro) telaInicial() bool {
	for {
		visual.LinhaDivisoria()
		fmt.Println(visual.Negrito + "1." + visual.Reset + " Entrar")
		fmt.Println(visual.Negrito + "2." + visual.Reset + " Cadastrar nova conta")
		fmt.Println(visual.Negrito + "0." + visual.Reset + " Sair")
		visual.LinhaDivisoria()

		switch s.ler("Opção: ") {
		case "1":
			if s.entrar() {
				return true
			}
		case "2":
			s.cadastrar()
		case "0":
			return false
		default:
			visual.MensagemAviso("Opção inválida.")
		}
	}
}

func (s *sessaoPassageiro) cadastrar() {
	visual.LinhaDivisoria()
	fmt.Println(visual.Negrito + "--- Cadastro de Passageiro ---" + visual.Reset)
	usuario := s.ler("Novo usuário: ")
	senha := s.ler("Senha: ")

	if usuario == "" || senha == "" {
		visual.MensagemErro("Usuário e senha não podem ser vazios.")
		return
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
		visual.MensagemErro(err.Error())
		return
	}

	if resp.Sucesso {
		visual.MensagemSucesso(resp.Mensagem)
	} else {
		visual.MensagemErro(resp.Mensagem)
	}
}

func (s *sessaoPassageiro) entrar() bool {
	visual.LinhaDivisoria()
	fmt.Println(visual.Negrito + "--- Autenticação ---" + visual.Reset)
	usuario := s.ler("Usuário: ")
	senha := s.ler("Senha: ")

	req := map[string]any{
		"tipo":  cliente_servidor.TipoLoginReq,
		"login": usuario,
		"senha": senha,
	}

	var resp cliente_servidor.RespostaLogin
	err := s.trocar(req, cliente_servidor.TipoLoginRes, &resp)
	if err != nil {
		visual.MensagemErro(err.Error())
		return false
	}
	if !resp.Sucesso {
		visual.MensagemErro(fmt.Sprintf("Falha no login: %s", resp.Mensagem))
		return false
	}
	if resp.TipoUsuario != string(modelos.UsuarioPassageiro) {
		visual.MensagemErro(fmt.Sprintf("Acesso negado: esta conta é de '%s'. Use a aplicação de motorista.", resp.TipoUsuario))
		return false
	}

	s.usuario = usuario
	visual.MensagemSucesso(fmt.Sprintf("Bem-vindo(a), %s!", s.usuario))
	s.verificarNotificacoes()
	return true
}

func (s *sessaoPassageiro) menuPrincipal() {
	for {
		visual.LinhaDivisoria()
		fmt.Printf(visual.Negrito+"Painel do Passageiro: %s%s\n", s.usuario, visual.Reset)
		visual.LinhaDivisoria()
		fmt.Println(visual.Negrito + "1." + visual.Reset + " Buscar e Reservar Itinerários (Consórcio)")
		fmt.Println(visual.Negrito + "2." + visual.Reset + " Minhas Reservas Confirmadas")
		fmt.Println(visual.Negrito + "3." + visual.Reset + " Cancelar Reserva")
		fmt.Println(visual.Negrito + "4." + visual.Reset + " Ver Notificações")
		fmt.Println(visual.Negrito + "0." + visual.Reset + " Sair")
		visual.LinhaDivisoria()

		switch s.ler("Opção: ") {
		case "1":
			s.buscarEReservar()
		case "2":
			s.listarReservas()
		case "3":
			s.cancelarReserva()
		case "4":
			s.verificarNotificacoes()
		case "0":
			visual.MensagemAviso("Encerrando sessão. Até logo!")
			return
		default:
			visual.MensagemAviso("Opção inválida.")
		}
	}
}

func (s *sessaoPassageiro) buscarEReservar() {
	visual.LinhaDivisoria()
	fmt.Println(visual.Negrito + "--- Busca de Itinerários no Consórcio ---" + visual.Reset)
	origem := s.ler("Cidade de Origem: ")
	destino := s.ler("Cidade de Destino: ")

	if origem == "" || destino == "" {
		visual.MensagemErro("Origem e destino são obrigatórios.")
		return
	}

	req := map[string]any{
		"tipo":    cliente_servidor.TipoBuscarItinerariosReq,
		"origem":  origem,
		"destino": destino,
	}

	var res cliente_servidor.RespostaBuscarItinerarios
	err := s.trocar(req, cliente_servidor.TipoBuscarItinerariosRes, &res)
	if err != nil {
		visual.MensagemErro(err.Error())
		return
	}

	if len(res.Itinerarios) == 0 {
		visual.MensagemAviso("Nenhum itinerário encontrado no consórcio para esta rota.")
		return
	}

	fmt.Println()
	colunas := []string{"Opção", "Preço Total", "Trechos", "Composição da Rota (Empresa / Segmentos)"}
	larguras := []int{7, 13, 9, 48}
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

	escolhaStr := s.ler("\nDigite o número da opção para reservar via 2PC (ou 0 para voltar): ")
	escolha, err := strconv.Atoi(escolhaStr)
	if err != nil || escolha <= 0 || escolha > len(res.Itinerarios) {
		return
	}

	itinerarioEscolhido := res.Itinerarios[escolha-1]

	if !s.confirmar(fmt.Sprintf("Confirmar reserva do Itinerário #%d por R$ %.2f?", escolha, itinerarioEscolhido.PrecoTotal)) {
		visual.MensagemAviso("Reserva cancelada pelo passageiro.")
		return
	}

	visual.MensagemAviso("Disparando Two-Phase Commit (2PC) com as empresas do consórcio...")

	reqReserva := map[string]any{
		"tipo":       cliente_servidor.TipoReservarItinerarioReq,
		"passageiro": s.usuario,
		"itinerario": itinerarioEscolhido,
	}

	var respRes cliente_servidor.RespostaReservar
	err = s.trocar(reqReserva, cliente_servidor.TipoReservarItinerarioRes, &respRes)
	if err != nil {
		visual.MensagemErro(err.Error())
		return
	}

	if respRes.Sucesso {
		visual.MensagemSucesso(fmt.Sprintf("Reserva atômica concluída com sucesso! Bilhete: #%d", respRes.ReservaID))
	} else {
		visual.MensagemErro(fmt.Sprintf("Falha na reserva atômica (GLOBAL ABORT): %s", respRes.Mensagem))
	}
}

func (s *sessaoPassageiro) buscarReservas() ([]modelos.Reserva, error) {
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
		return nil, err
	}
	if !res.Sucesso {
		return nil, errors.New(res.Mensagem)
	}
	return res.Reservas, nil
}

func (s *sessaoPassageiro) listarReservas() {
	reservas, err := s.buscarReservas()
	if err != nil {
		visual.MensagemErro(err.Error())
		return
	}

	if len(reservas) == 0 {
		visual.MensagemAviso("Você ainda não possui reservas confirmadas.")
		return
	}

	fmt.Println()
	colunas := []string{"Bilhete", "TxID (2PC)", "Preço Total", "Trechos Confirmados"}
	larguras := []int{9, 24, 13, 35}
	visual.TabelaCabecalho(colunas, larguras)

	for _, r := range reservas {
		var resumo []string
		for _, t := range r.Trechos {
			resumo = append(resumo, fmt.Sprintf("%s (%s->%s)", t.Empresa, t.Origem, t.Destino))
		}
		visual.TabelaLinha([]string{
			fmt.Sprintf("#%d", r.ID),
			r.TxID,
			fmt.Sprintf("R$ %.2f", r.PrecoTotal),
			strings.Join(resumo, ", "),
		}, larguras)
	}
	visual.TabelaRodape(larguras)
}

func (s *sessaoPassageiro) cancelarReserva() {
	reservas, err := s.buscarReservas()
	if err != nil {
		visual.MensagemErro(err.Error())
		return
	}

	if len(reservas) == 0 {
		visual.MensagemAviso("Nenhuma reserva disponível para cancelamento.")
		return
	}

	s.listarReservas()

	idStr := s.ler("\nNúmero do bilhete a cancelar (ou 0 para voltar): ")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		return
	}

	if !s.confirmar(fmt.Sprintf("Tem certeza que deseja cancelar a reserva #%d?", id)) {
		visual.MensagemAviso("Cancelamento abortado.")
		return
	}

	req := map[string]any{
		"tipo":       cliente_servidor.TipoCancelarReservaReq,
		"reserva_id": id,
		"passageiro": s.usuario,
	}

	var resp cliente_servidor.RespostaSimples
	err = s.trocar(req, cliente_servidor.TipoCancelarReservaRes, &resp)
	if err != nil {
		visual.MensagemErro(err.Error())
		return
	}

	if resp.Sucesso {
		visual.MensagemSucesso(resp.Mensagem)
	} else {
		visual.MensagemErro(resp.Mensagem)
	}
}

func (s *sessaoPassageiro) verificarNotificacoes() {
	req := map[string]any{
		"tipo":       cliente_servidor.TipoConsultarNotificacoesReq,
		"passageiro": s.usuario,
	}

	var res cliente_servidor.RespostaConsultarNotificacoes
	err := s.trocar(req, cliente_servidor.TipoConsultarNotificacoesRes, &res)
	if err != nil {
		return
	}

	if len(res.Notificacoes) > 0 {
		visual.LinhaDivisoria()
		fmt.Println(visual.Negrito + "=== AVISOS IMPORTANTES RECEBIDOS ===" + visual.Reset)
		for _, n := range res.Notificacoes {
			visual.MensagemAviso(fmt.Sprintf("[%s] %s", n.Data, n.Mensagem))
		}
		visual.LinhaDivisoria()
	}
}
