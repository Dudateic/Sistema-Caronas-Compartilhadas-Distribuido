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

type sessaoMotorista struct {
	endereco string
	cli      *cliente_servidor.ClienteTCP
	usuario  string
}

func (s *sessaoMotorista) confirmar(pergunta string) bool {
	r := strings.ToLower(lerEntrada(pergunta + " (s/n): "))
	return r == "s" || r == "sim"
}

func (s *sessaoMotorista) reconectar() error {
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

func (s *sessaoMotorista) garantirConexao() error {
	select {
	case err := <-s.cli.Erros:
		visual.MensagemErro(fmt.Sprintf("Conexão perdida (%v).", err))
		return s.reconectar()
	default:
		return nil
	}
}

func (s *sessaoMotorista) trocar(req any, tipoEsperado string, destino any) error {
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
			return s.falhaDeConexao(errors.New("tempo esgotado aguardando o servidor"))
		}
	}
}

func (s *sessaoMotorista) falhaDeConexao(causa error) error {
	visual.MensagemErro(fmt.Sprintf("%v", causa))
	if err := s.reconectar(); err != nil {
		return err
	}
	return errors.New("a resposta se perdeu; a operação pode ter sido concluída, confira em 'Minhas caronas'")
}

func main() {
	LimparTela()
	visual.ExibirCabecalho("VAIJUNTO - MODULO DO MOTORISTA")

	padrao := os.Getenv("SERVIDOR")
	if padrao == "" {
		padrao = "localhost:8081"
	}

	flagEndereco := flag.String("servidor", "", "servidor da sua empresa (host:porta)")
	flag.Parse()

	endereco := *flagEndereco
	if endereco == "" {
		fmt.Printf("Digite o IP:Porta do servidor da sua empresa\n")
		endereco = lerEntrada(fmt.Sprintf("(Pressione ENTER para usar %s): ", padrao))
		if endereco == "" {
			endereco = padrao
		}
	}

	fmt.Printf("\nConectando ao servidor em %s...\n", endereco)
	cli, err := cliente_servidor.ConectarTCP(endereco, 5*time.Second)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao conectar: %v", err))
		return
	}
	defer func() { _ = cli.Fechar() }()

	visual.MensagemSucesso("Conexao estabelecida com sucesso!")
	time.Sleep(1 * time.Second)

	s := &sessaoMotorista{
		endereco: endereco,
		cli:      cli,
	}

	if !s.telaAcesso() {
		fmt.Println("Operacao encerrada pelo usuario. Ate logo!")
		return
	}

	s.menuPrincipalMotorista()
}

func (s *sessaoMotorista) telaAcesso() bool {
	for {
		LimparTela()
		visual.ExibirCabecalho("AUTENTICACAO DO MOTORISTA")
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

			if resp.TipoUsuario != string(modelos.UsuarioMotorista) {
				visual.MensagemErro(fmt.Sprintf("Acesso negado: esta conta e de '%s'. Use o modulo de passageiro.", resp.TipoUsuario))
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
				"tipo_usuario": string(modelos.UsuarioMotorista),
			}

			var resp cliente_servidor.RespostaSimples
			err := s.trocar(req, cliente_servidor.TipoCadastroRes, &resp)
			if err != nil {
				visual.MensagemErro(fmt.Sprintf("Falha ao registrar usuario: %v", err))
				lerEntrada("\nPressione ENTER para continuar...")
				continue
			}

			if resp.Sucesso {
				visual.MensagemSucesso("Conta de motorista criada! Faca login na opcao 1.")
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

func (s *sessaoMotorista) menuPrincipalMotorista() {
	for {
		LimparTela()
		visual.ExibirCabecalho(fmt.Sprintf("PAINEL DO MOTORISTA: %s", s.usuario))
		fmt.Println("1. Publicar Nova Carona (com trechos contiguos)")
		fmt.Println("2. Minhas Caronas Publicadas")
		fmt.Println("3. Cancelar Carona")
		fmt.Println("0. Sair e Desconectar")
		fmt.Println()

		opcao := lerEntrada("Selecione a opcao desejada: ")

		switch opcao {
		case "1":
			s.acaoPublicar()
		case "2":
			s.acaoListar()
		case "3":
			s.acaoCancelar()
		case "0":
			fmt.Println("Desconectando do servidor... Ate logo!")
			return
		default:
			visual.MensagemAviso("Opcao invalida. Digite um numero entre 0 e 3.")
			lerEntrada("\nPressione ENTER para continuar...")
		}
	}
}

func lerRota(texto string) ([]string, error) {
	sep := ","
	if strings.Contains(texto, ";") {
		sep = ";"
	}
	var rota []string
	vistas := map[string]bool{}
	for _, c := range strings.Split(texto, sep) {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		chave := strings.ToLower(c)
		if vistas[chave] {
			return nil, fmt.Errorf("a cidade %q aparece mais de uma vez na rota", c)
		}
		vistas[chave] = true
		rota = append(rota, c)
	}
	if len(rota) < 2 {
		return nil, errors.New("a rota precisa de pelo menos 2 cidades (origem e destino)")
	}
	return rota, nil
}

func (s *sessaoMotorista) acaoPublicar() {
	LimparTela()
	visual.ExibirCabecalho("PUBLICAR NOVA CARONA")

	rota, err := lerRota(lerEntrada("Rota (cidades na ordem separadas por ';' ou ','): "))
	if err != nil {
		visual.MensagemErro(err.Error())
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	dataTxt := lerEntrada("Data da viagem (AAAA-MM-DD): ")
	data, err := time.Parse("2006-01-02", dataTxt)
	if err != nil {
		visual.MensagemErro("Data invalida. Use o formato AAAA-MM-DD.")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}
	hoje := time.Now().Truncate(24 * time.Hour)
	if data.Before(hoje.AddDate(0, 0, -1)) {
		visual.MensagemErro("A data da viagem ja passou.")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	horaTxt := lerEntrada("Horario de saida (HH:MM): ")
	hora, err := time.Parse("15:04", horaTxt)
	if err != nil {
		visual.MensagemErro("Horario invalido. Use o formato HH:MM.")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	assentos, err := strconv.Atoi(lerEntrada("Total de assentos disponiveis: "))
	if err != nil || assentos <= 0 {
		visual.MensagemErro("Numero de assentos invalido (deve ser maior que zero).")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	preco, err := strconv.ParseFloat(strings.ReplaceAll(lerEntrada("Preco por trecho (R$): "), ",", "."), 64)
	if err != nil || preco <= 0 {
		visual.MensagemErro("Preco invalido (deve ser maior que zero).")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	fmt.Printf("\nResumo da viagem: %s | %s as %s | %d vaga(s) | R$ %.2f por trecho\n",
		strings.Join(rota, " -> "), data.Format("2006-01-02"), hora.Format("15:04"), assentos, preco)

	if !s.confirmar("Confirmar publicacao desta carona?") {
		visual.MensagemAviso("Publicacao cancelada pelo usuario.")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	req := map[string]any{
		"tipo":             cliente_servidor.TipoPublicarCaronaReq,
		"motorista":        s.usuario,
		"rota":             rota,
		"data":             data.Format("2006-01-02"),
		"horario":          hora.Format("15:04"),
		"assentos_totais":  assentos,
		"preco_por_trecho": preco,
	}

	var resp cliente_servidor.RespostaPublicarCarona
	err = s.trocar(req, cliente_servidor.TipoPublicarCaronaRes, &resp)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao comunicar com o servidor: %v", err))
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	if resp.Sucesso {
		visual.MensagemSucesso(fmt.Sprintf("%s (Carona #%d registrada no consorcio)", resp.Mensagem, resp.CaronaID))
	} else {
		visual.MensagemErro(fmt.Sprintf("Nao foi possivel publicar: %s", resp.Mensagem))
	}
	lerEntrada("\nPressione ENTER para voltar ao menu...")
}

func (s *sessaoMotorista) buscarCaronas() ([]modelos.Carona, error) {
	req := map[string]any{
		"tipo":      cliente_servidor.TipoConsultarCaronasReq,
		"motorista": s.usuario,
	}

	var resp cliente_servidor.RespostaConsultarCaronas
	err := s.trocar(req, cliente_servidor.TipoConsultarCaronasRes, &resp)
	if err != nil {
		return nil, err
	}
	if !resp.Sucesso {
		return nil, errors.New(resp.Mensagem)
	}
	return resp.Caronas, nil
}

func passageirosDistintos(c modelos.Carona) int {
	vistos := map[string]bool{}
	for _, t := range c.Trechos {
		for _, p := range t.Passageiros {
			vistos[p] = true
		}
	}
	return len(vistos)
}

func imprimirCaronaTabela(c modelos.Carona) {
	fmt.Printf("\n"+visual.Negrito+"Carona #%d"+visual.Reset+" | %s as %s | %s\n", c.ID, c.Data, c.Horario, strings.Join(c.Rota, " -> "))
	fmt.Printf("Vagas totais: %d | R$ %.2f por trecho | Passageiros confirmados: %d\n", c.AssentosTotais, c.PrecoPorTrecho, passageirosDistintos(c))

	colunas := []string{"Origem", "Destino", "Ocupacao", "Passageiros Confirmados"}
	larguras := []int{16, 16, 10, 32}
	visual.TabelaCabecalho(colunas, larguras)

	for _, t := range c.Trechos {
		ocupados := c.AssentosTotais - t.AssentosLivres
		listaPassageiros := "-"
		if len(t.Passageiros) > 0 {
			listaPassageiros = strings.Join(t.Passageiros, ", ")
		}
		visual.TabelaLinha([]string{
			t.Origem,
			t.Destino,
			fmt.Sprintf("%d/%d", ocupados, c.AssentosTotais),
			listaPassageiros,
		}, larguras)
	}
	visual.TabelaRodape(larguras)
}

func (s *sessaoMotorista) acaoListar() {
	LimparTela()
	visual.ExibirCabecalho("SUAS CARONAS ANUNCIADAS")

	caronas, err := s.buscarCaronas()
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao buscar caronas: %v", err))
		lerEntrada("\nPressione ENTER para continuar...")
		return
	}
	if len(caronas) == 0 {
		visual.MensagemAviso("Voce nao possui nenhuma carona publicada.")
		lerEntrada("\nPressione ENTER para continuar...")
		return
	}
	for _, c := range caronas {
		imprimirCaronaTabela(c)
	}
	lerEntrada("\nPressione ENTER para voltar ao menu...")
}

func (s *sessaoMotorista) acaoCancelar() {
	LimparTela()
	visual.ExibirCabecalho("CANCELAR CARONA")

	caronas, err := s.buscarCaronas()
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao buscar caronas: %v", err))
		lerEntrada("\nPressione ENTER para continuar...")
		return
	}
	if len(caronas) == 0 {
		visual.MensagemAviso("Voce nao possui caronas para cancelar.")
		lerEntrada("\nPressione ENTER para continuar...")
		return
	}
	for _, c := range caronas {
		imprimirCaronaTabela(c)
	}

	idStr := lerEntrada("\nNumero da carona a cancelar (ou 0 para voltar): ")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		return
	}

	var alvo *modelos.Carona
	for i := range caronas {
		if caronas[i].ID == id {
			alvo = &caronas[i]
		}
	}
	if alvo == nil {
		visual.MensagemErro("Carona nao encontrada entre as suas publicacoes.")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	if n := passageirosDistintos(*alvo); n > 0 {
		visual.MensagemAviso(fmt.Sprintf("ATENCAO: %d passageiro(s) possuem bilhetes nesta carona.\nO itinerario completo de cada um sera cancelado no consorcio e eles serao avisados.", n))
	}
	if !s.confirmar(fmt.Sprintf("Cancelar a carona #%d definitivamente?", id)) {
		visual.MensagemAviso("Cancelamento abortado.")
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	req := map[string]any{
		"tipo":      cliente_servidor.TipoCancelarCaronaReq,
		"carona_id": id,
		"motorista": s.usuario,
	}

	var resp cliente_servidor.RespostaSimples
	err = s.trocar(req, cliente_servidor.TipoCancelarCaronaRes, &resp)
	if err != nil {
		visual.MensagemErro(fmt.Sprintf("Falha ao comunicar cancelamento: %v", err))
		lerEntrada("\nPressione ENTER para voltar...")
		return
	}

	if resp.Sucesso {
		visual.MensagemSucesso(resp.Mensagem)
	} else {
		visual.MensagemErro(resp.Mensagem)
	}
	lerEntrada("\nPressione ENTER para voltar ao menu...")
}
