// go run main.go -servidor localhost:8081
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

	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/conexao"
	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
)

type sessao struct {
	endereco string
	cli      *conexao.ClienteTCP
	teclado  *bufio.Reader
	usuario  string
}

func (s *sessao) ler(prompt string) string {
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

func (s *sessao) confirmar(pergunta string) bool {
	r := strings.ToLower(s.ler(pergunta + " (s/n): "))
	return r == "s" || r == "sim"
}

func (s *sessao) reconectar() error {
	if s.cli != nil {
		_ = s.cli.Fechar()
	}
	var ultimo error
	for i := 1; i <= tentativasReconexao; i++ {
		fmt.Printf("[REDE] Reconectando a %s (tentativa %d/%d)...\n", s.endereco, i, tentativasReconexao)
		cli, err := conexao.ConectarTCP(s.endereco, 2*time.Second)
		if err == nil {
			s.cli = cli
			fmt.Println("[REDE] Conexao restabelecida.")
			return nil
		}
		ultimo = err
		time.Sleep(intervaloReconexao)
	}
	return fmt.Errorf("servidor indisponivel: %w", ultimo)
}

func (s *sessao) garantirConexao() error {
	select {
	case err := <-s.cli.Erros:
		fmt.Printf("[REDE] Conexao perdida (%v).\n", err)
		return s.reconectar()
	default:
		return nil
	}
}

func (s *sessao) trocar(req any, tipoEsperado string, destino any) error {
	if err := s.garantirConexao(); err != nil {
		return err
	}

	// Limpa dados residuais pendentes no buffer do canal
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
				return fmt.Errorf("resposta invalida do servidor: %w", err)
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
	fmt.Printf("\n[AVISO] %v\n", causa)
	if err := s.reconectar(); err != nil {
		return err
	}
	return errors.New("a resposta se perdeu; a operacao pode ter sido concluida, confira em 'Minhas caronas'")
}

func main() {
	padrao := os.Getenv("SERVIDOR")
	if padrao == "" {
		padrao = conexao.EnderecoPadrao
	}
	endereco := flag.String("servidor", padrao, "servidor da sua empresa (host:porta)")
	flag.Parse()

	cli, err := conexao.ConectarTCP(*endereco, 5*time.Second)
	if err != nil {
		fmt.Println("Erro:", err)
		os.Exit(1)
	}
	s := &sessao{endereco: *endereco, cli: cli, teclado: bufio.NewReader(os.Stdin)}
	defer func() { _ = s.cli.Fechar() }()

	fmt.Printf("Conectado a %s\n", *endereco)
	if !s.telaInicial() {
		return
	}
	s.menuPrincipal()
}

func (s *sessao) telaInicial() bool {
	for {
		fmt.Println("\n=== VAIJUNTO - MOTORISTA ===")
		fmt.Println("1. Entrar")
		fmt.Println("2. Cadastrar")
		fmt.Println("0. Sair")
		switch s.ler("Opcao: ") {
		case "1":
			if s.entrar() {
				return true
			}
		case "2":
			s.cadastrar()
		case "0":
			return false
		default:
			fmt.Println("Opcao invalida.")
		}
	}
}

func (s *sessao) cadastrar() {
	usuario, senha := s.ler("Novo usuario: "), s.ler("Senha: ")
	if usuario == "" || senha == "" {
		fmt.Println("Usuario e senha nao podem ser vazios.")
		return
	}
	var resp protocolo.RespostaSimples
	err := s.trocar(protocolo.RegistroUsuario{
		Tipo:        protocolo.TipoCadastroReq,
		Usuario:     usuario,
		Senha:       senha,
		TipoUsuario: protocolo.PerfilMotorista,
	}, protocolo.TipoCadastroRes, &resp)
	if err != nil {
		fmt.Println("Erro:", err)
		return
	}
	fmt.Println(resp.Mensagem)
}

func (s *sessao) entrar() bool {
	usuario, senha := s.ler("Usuario: "), s.ler("Senha: ")
	var resp protocolo.RespostaLogin
	err := s.trocar(protocolo.CredencialLogin{
		Tipo:    protocolo.TipoLoginReq,
		Usuario: usuario,
		Senha:   senha,
	}, protocolo.TipoLoginRes, &resp)
	if err != nil {
		fmt.Println("Erro:", err)
		return false
	}
	if !resp.Sucesso {
		fmt.Println("Falha no login:", resp.Mensagem)
		return false
	}
	if resp.TipoUsuario != protocolo.PerfilMotorista {
		fmt.Printf("Acesso negado: esta conta e de '%s'. Use a aplicacao de passageiro.\n", resp.TipoUsuario)
		return false
	}
	s.usuario = usuario
	fmt.Println(resp.Mensagem)
	return true
}

func (s *sessao) menuPrincipal() {
	for {
		fmt.Printf("\n=== MOTORISTA: %s ===\n", s.usuario)
		fmt.Println("1. Publicar carona")
		fmt.Println("2. Minhas caronas")
		fmt.Println("3. Cancelar carona")
		fmt.Println("0. Sair")
		switch s.ler("Opcao: ") {
		case "1":
			s.publicar()
		case "2":
			s.listar()
		case "3":
			s.cancelar()
		case "0":
			fmt.Println("Ate logo!")
			return
		default:
			fmt.Println("Opcao invalida.")
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
	fmt.Println("\n--- Publicar carona ---")

	rota, err := lerRota(s.ler("Rota, cidades na ordem separadas por ';' (ex: Salvador;Feira de Santana;Serrinha): "))
	if err != nil {
		fmt.Println("Erro:", err)
		return
	}

	dataTxt := s.ler("Data da viagem (AAAA-MM-DD): ")
	data, err := time.Parse("2006-01-02", dataTxt)
	if err != nil {
		fmt.Println("Data invalida. Use o formato AAAA-MM-DD.")
		return
	}
	hoje := time.Now().Truncate(24 * time.Hour)
	if data.Before(hoje.AddDate(0, 0, -1)) {
		fmt.Println("A data da viagem ja passou.")
		return
	}

	horaTxt := s.ler("Horario de saida (HH:MM): ")
	hora, err := time.Parse("15:04", horaTxt)
	if err != nil {
		fmt.Println("Horario invalido. Use o formato HH:MM.")
		return
	}

	assentos, err := strconv.Atoi(s.ler("Assentos disponiveis: "))
	if err != nil || assentos <= 0 {
		fmt.Println("Numero de assentos invalido (use um inteiro maior que zero).")
		return
	}

	preco, err := strconv.ParseFloat(strings.ReplaceAll(s.ler("Preco por trecho (R$): "), ",", "."), 64)
	if err != nil || preco <= 0 {
		fmt.Println("Preco invalido (use um numero maior que zero).")
		return
	}

	fmt.Printf("\nResumo: %s | %s as %s | %d assento(s) | R$ %.2f por trecho\n",
		strings.Join(rota, " -> "), data.Format("2006-01-02"), hora.Format("15:04"), assentos, preco)
	if !s.confirmar("Confirmar publicacao?") {
		fmt.Println("Publicacao cancelada.")
		return
	}

	var resp protocolo.RespostaPublicarCarona
	err = s.trocar(protocolo.PublicacaoCarona{
		Tipo:           protocolo.TipoPublicarCaronaReq,
		Motorista:      s.usuario,
		Rota:           rota,
		Data:           data.Format("2006-01-02"),
		Horario:        hora.Format("15:04"),
		AssentosTotais: assentos,
		PrecoPorTrecho: preco,
	}, protocolo.TipoPublicarCaronaRes, &resp)
	if err != nil {
		fmt.Println("Erro:", err)
		return
	}
	if resp.Sucesso {
		fmt.Printf("%s (carona #%d)\n", resp.Mensagem, resp.CaronaID)
	} else {
		fmt.Println("Nao foi possivel publicar:", resp.Mensagem)
	}
}

func (s *sessao) buscarCaronas() ([]protocolo.Carona, error) {
	var resp protocolo.RespostaConsultarCaronas
	err := s.trocar(protocolo.ConsultaCaronasMotorista{
		Tipo:      protocolo.TipoConsultarCaronasReq,
		Motorista: s.usuario,
	}, protocolo.TipoConsultarCaronasRes, &resp)
	if err != nil {
		return nil, err
	}
	if !resp.Sucesso {
		return nil, errors.New(resp.Mensagem)
	}
	return resp.Caronas, nil
}

func passageirosDistintos(c protocolo.Carona) int {
	vistos := map[string]bool{}
	for _, t := range c.Trechos {
		for _, p := range t.Passageiros {
			vistos[p] = true
		}
	}
	return len(vistos)
}

func imprimirCarona(c protocolo.Carona) {
	fmt.Printf("\nCarona #%d | %s as %s | %s\n", c.ID, c.Data, c.Horario, strings.Join(c.Rota, " -> "))
	fmt.Printf("  %d assento(s) | R$ %.2f por trecho | %d passageiro(s)\n", c.AssentosTotais, c.PrecoPorTrecho, passageirosDistintos(c))
	for _, t := range c.Trechos {
		ocupados := c.AssentosTotais - t.AssentosLivres
		linha := fmt.Sprintf("  - %s -> %s: %d/%d ocupado(s)", t.Origem, t.Destino, ocupados, c.AssentosTotais)
		if len(t.Passageiros) > 0 {
			linha += " [" + strings.Join(t.Passageiros, ", ") + "]"
		}
		fmt.Println(linha)
	}
}

func (s *sessao) listar() {
	caronas, err := s.buscarCaronas()
	if err != nil {
		fmt.Println("Erro:", err)
		return
	}
	if len(caronas) == 0 {
		fmt.Println("\nVoce nao tem caronas publicadas.")
		return
	}
	for _, c := range caronas {
		imprimirCarona(c)
	}
}

func (s *sessao) cancelar() {
	caronas, err := s.buscarCaronas()
	if err != nil {
		fmt.Println("Erro:", err)
		return
	}
	if len(caronas) == 0 {
		fmt.Println("\nVoce nao tem caronas para cancelar.")
		return
	}
	for _, c := range caronas {
		imprimirCarona(c)
	}

	id, err := strconv.Atoi(s.ler("\nNumero da carona a cancelar (0 para voltar): "))
	if err != nil || id <= 0 {
		return
	}
	var alvo *protocolo.Carona
	for i := range caronas {
		if caronas[i].ID == id {
			alvo = &caronas[i]
		}
	}
	if alvo == nil {
		fmt.Println("Carona nao encontrada entre as suas.")
		return
	}

	if n := passageirosDistintos(*alvo); n > 0 {
		fmt.Printf("ATENCAO: %d passageiro(s) tem reserva nesta carona. O itinerario completo de cada um\n"+
			"(inclusive trechos de outras empresas) sera cancelado e eles serao avisados.\n", n)
	}
	if !s.confirmar(fmt.Sprintf("Cancelar a carona #%d?", id)) {
		fmt.Println("Cancelamento abortado.")
		return
	}

	var resp protocolo.RespostaSimples
	err = s.trocar(protocolo.CancelamentoCarona{
		Tipo:      protocolo.TipoCancelarCaronaReq,
		CaronaID:  id,
		Motorista: s.usuario,
	}, protocolo.TipoCancelarCaronaRes, &resp)
	if err != nil {
		fmt.Println("Erro:", err)
		return
	}
	fmt.Println(resp.Mensagem)
}

const (
	tentativasReconexao = 3
	intervaloReconexao  = time.Second
	timeoutResposta     = 10 * time.Second
)
