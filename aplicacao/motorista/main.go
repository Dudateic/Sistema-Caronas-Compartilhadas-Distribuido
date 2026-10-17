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

type sessao struct {
	endereco string
	cli      *cliente_servidor.ClienteTCP
	teclado  *bufio.Reader
	usuario  string
}

func (s *sessao) ler(prompt string) string {
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

func (s *sessao) confirmar(pergunta string) bool {
	r := strings.ToLower(s.ler(pergunta + " (s/n): "))
	return r == "s" || r == "sim"
}

func (s *sessao) reconectar() error {
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

func (s *sessao) garantirConexao() error {
	select {
	case err := <-s.cli.Erros:
		visual.MensagemErro(fmt.Sprintf("Conexão perdida (%v).", err))
		return s.reconectar()
	default:
		return nil
	}
}

func (s *sessao) trocar(req any, tipoEsperado string, destino any) error {
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
			return s.falhaDeConexao(errors.New("tempo esgotado aguardando o servidor"))
		}
	}
}

func (s *sessao) falhaDeConexao(causa error) error {
	visual.MensagemErro(fmt.Sprintf("%v", causa))
	if err := s.reconectar(); err != nil {
		return err
	}
	return errors.New("a resposta se perdeu; a operação pode ter sido concluída, confira em 'Minhas caronas'")
}

func main() {
	visual.ExibirCabecalho("VaiJunto - Terminal Motorista")

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
	s := &sessao{endereco: *endereco, cli: cli, teclado: bufio.NewReader(os.Stdin)}
	defer func() { _ = s.cli.Fechar() }()

	visual.MensagemSucesso(fmt.Sprintf("Conectado ao servidor %s com sucesso!", *endereco))

	if !s.telaInicial() {
		return
	}
	s.menuPrincipal()
}

func (s *sessao) telaInicial() bool {
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

func (s *sessao) cadastrar() {
	visual.LinhaDivisoria()
	fmt.Println(visual.Negrito + "--- Cadastro de Motorista ---" + visual.Reset)
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
		"tipo_usuario": string(modelos.UsuarioMotorista),
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

func (s *sessao) entrar() bool {
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
	if resp.TipoUsuario != string(modelos.UsuarioMotorista) {
		visual.MensagemErro(fmt.Sprintf("Acesso negado: esta conta é de '%s'. Use a aplicação de passageiro.", resp.TipoUsuario))
		return false
	}

	s.usuario = usuario
	visual.MensagemSucesso(fmt.Sprintf("Bem-vindo(a), %s!", s.usuario))
	return true
}

func (s *sessao) menuPrincipal() {
	for {
		visual.LinhaDivisoria()
		fmt.Printf(visual.Negrito+"Painel do Motorista: %s%s\n", s.usuario, visual.Reset)
		visual.LinhaDivisoria()
		fmt.Println(visual.Negrito + "1." + visual.Reset + " Publicar carona")
		fmt.Println(visual.Negrito + "2." + visual.Reset + " Minhas caronas")
		fmt.Println(visual.Negrito + "3." + visual.Reset + " Cancelar carona")
		fmt.Println(visual.Negrito + "0." + visual.Reset + " Sair")
		visual.LinhaDivisoria()

		switch s.ler("Opção: ") {
		case "1":
			s.publicar()
		case "2":
			s.listar()
		case "3":
			s.cancelar()
		case "0":
			visual.MensagemAviso("Encerrando sessão. Até logo!")
			return
		default:
			visual.MensagemAviso("Opção inválida.")
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

func (s *sessao) publicar() {
	visual.LinhaDivisoria()
	fmt.Println(visual.Negrito + "--- Publicar Nova Carona ---" + visual.Reset)

	rota, err := lerRota(s.ler("Rota (cidades separadas por ';' ou ','): "))
	if err != nil {
		visual.MensagemErro(err.Error())
		return
	}

	dataTxt := s.ler("Data da viagem (AAAA-MM-DD): ")
	data, err := time.Parse("2006-01-02", dataTxt)
	if err != nil {
		visual.MensagemErro("Data inválida. Use o formato AAAA-MM-DD.")
		return
	}
	hoje := time.Now().Truncate(24 * time.Hour)
	if data.Before(hoje.AddDate(0, 0, -1)) {
		visual.MensagemErro("A data da viagem já passou.")
		return
	}

	horaTxt := s.ler("Horário de saída (HH:MM): ")
	hora, err := time.Parse("15:04", horaTxt)
	if err != nil {
		visual.MensagemErro("Horário inválido. Use o formato HH:MM.")
		return
	}

	assentos, err := strconv.Atoi(s.ler("Total de assentos disponíveis: "))
	if err != nil || assentos <= 0 {
		visual.MensagemErro("Número de assentos inválido (deve ser maior que zero).")
		return
	}

	preco, err := strconv.ParseFloat(strings.ReplaceAll(s.ler("Preço por trecho (R$): "), ",", "."), 64)
	if err != nil || preco <= 0 {
		visual.MensagemErro("Preço inválido (deve ser maior que zero).")
		return
	}

	fmt.Printf("\nResumo: %s | %s às %s | %d vaga(s) | R$ %.2f por trecho\n",
		strings.Join(rota, " -> "), data.Format("2006-01-02"), hora.Format("15:04"), assentos, preco)
	if !s.confirmar("Confirmar publicação?") {
		visual.MensagemAviso("Publicação cancelada pelo usuário.")
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
		visual.MensagemErro(err.Error())
		return
	}

	if resp.Sucesso {
		visual.MensagemSucesso(fmt.Sprintf("%s (Carona #%d anunciada com trechos contíguos)", resp.Mensagem, resp.CaronaID))
	} else {
		visual.MensagemErro(fmt.Sprintf("Não foi possível publicar: %s", resp.Mensagem))
	}
}

func (s *sessao) buscarCaronas() ([]modelos.Carona, error) {
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
	fmt.Printf("\n"+visual.Negrito+"Carona #%d"+visual.Reset+" | %s às %s | %s\n", c.ID, c.Data, c.Horario, strings.Join(c.Rota, " -> "))
	fmt.Printf("Vagas totais: %d | R$ %.2f por trecho | Passageiros confirmados: %d\n", c.AssentosTotais, c.PrecoPorTrecho, passageirosDistintos(c))

	colunas := []string{"Origem", "Destino", "Ocupação", "Passageiros"}
	larguras := []int{16, 16, 10, 30}
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

func (s *sessao) listar() {
	caronas, err := s.buscarCaronas()
	if err != nil {
		visual.MensagemErro(err.Error())
		return
	}
	if len(caronas) == 0 {
		visual.MensagemAviso("Você não tem caronas publicadas.")
		return
	}
	for _, c := range caronas {
		imprimirCaronaTabela(c)
	}
}

func (s *sessao) cancelar() {
	caronas, err := s.buscarCaronas()
	if err != nil {
		visual.MensagemErro(err.Error())
		return
	}
	if len(caronas) == 0 {
		visual.MensagemAviso("Você não tem caronas para cancelar.")
		return
	}
	for _, c := range caronas {
		imprimirCaronaTabela(c)
	}

	id, err := strconv.Atoi(s.ler("\nNúmero da carona a cancelar (0 para voltar): "))
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
		visual.MensagemErro("Carona não encontrada entre as suas publicações.")
		return
	}

	if n := passageirosDistintos(*alvo); n > 0 {
		visual.MensagemAviso(fmt.Sprintf("ATENÇÃO: %d passageiro(s) possuem reserva nesta carona.\nO itinerário completo de cada um será cancelado e eles serão notificados.", n))
	}
	if !s.confirmar(fmt.Sprintf("Cancelar a carona #%d definitivamente?", id)) {
		visual.MensagemAviso("Cancelamento abortado.")
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
		visual.MensagemErro(err.Error())
		return
	}

	if resp.Sucesso {
		visual.MensagemSucesso(resp.Mensagem)
	} else {
		visual.MensagemErro(resp.Mensagem)
	}
}
