package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/conexao"
	"Sistema-Caronas-Compartilhadas-Distribuido/comunicacao/protocolo"
)

var mutexUsuarios sync.Mutex

const ArquivoUsuarios = "usuarios.json"

type UsuarioCadastrado struct {
	TipoUsuario string `json:"tipo"`
	IDUsuario   string `json:"id_usuario"`
	Usuario     string `json:"usuario"`
	Senha       string `json:"senha"`
}

func hashSimples(senha string) string {
	hash := sha256.Sum256([]byte(senha))
	return hex.EncodeToString(hash[:])
}

func gerarID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func carregarUsuariosDoBanco() ([]UsuarioCadastrado, error) {
	var listaUsuarios []UsuarioCadastrado
	if err := CarregarJSON(ArquivoUsuarios, &listaUsuarios); err != nil {
		return nil, fmt.Errorf("falha ao carregar usuarios via persistencia: %w", err)
	}
	return listaUsuarios, nil
}

func salvarUsuariosNoBanco(usuarios []UsuarioCadastrado) error {
	if err := SalvarJSON(ArquivoUsuarios, usuarios); err != nil {
		return fmt.Errorf("falha ao persistir lista de usuarios: %w", err)
	}
	return nil
}

func ProcessarCadastro(conn net.Conn, dadosBrutos string) {
	var req protocolo.RegistroUsuario
	if err := json.Unmarshal([]byte(dadosBrutos), &req); err != nil {
		responderCadastro(conn, false, "Formato de cadastro invalido")
		return
	}

	if req.Usuario == "" || req.Senha == "" {
		responderCadastro(conn, false, "Usuario e senha nao podem ser vazios")
		return
	}

	if req.TipoUsuario != protocolo.PerfilMotorista && req.TipoUsuario != protocolo.PerfilPassageiro {
		responderCadastro(conn, false, "Perfil de usuario invalido")
		return
	}

	mutexUsuarios.Lock()
	defer mutexUsuarios.Unlock()

	usuarios, err := carregarUsuariosDoBanco()
	if err != nil {
		fmt.Printf("[CADASTRO] Erro ao abrir banco: %v\n", err)
		responderCadastro(conn, false, "Erro interno ao consultar banco de dados")
		return
	}

	for _, u := range usuarios {
		if u.Usuario == req.Usuario {
			responderCadastro(conn, false, "Nome de usuario ja em uso")
			return
		}
	}

	novoUsuario := UsuarioCadastrado{
		IDUsuario:   gerarID(),
		TipoUsuario: req.TipoUsuario,
		Usuario:     req.Usuario,
		Senha:       hashSimples(req.Senha),
	}

	usuarios = append(usuarios, novoUsuario)
	if err := salvarUsuariosNoBanco(usuarios); err != nil {
		fmt.Printf("[CADASTRO] Erro ao gravar banco: %v\n", err)
		responderCadastro(conn, false, "Erro interno ao salvar novo usuario")
		return
	}

	fmt.Printf("[CADASTRO] Novo usuario registrado: '%s' (%s)\n", req.Usuario, req.TipoUsuario)
	responderCadastro(conn, true, "Usuario cadastrado com sucesso!")
}

func responderCadastro(conn net.Conn, sucesso bool, mensagem string) {
	resp := protocolo.RespostaSimples{
		Tipo:     "cadastro_resposta",
		Sucesso:  sucesso,
		Mensagem: mensagem,
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func ProcessarLogin(conn net.Conn, dadosBrutos string) {
	var req protocolo.CredencialLogin
	if err := json.Unmarshal([]byte(dadosBrutos), &req); err != nil {
		responderLogin(conn, false, "Formato de login invalido", "", "")
		return
	}

	fmt.Printf("[AUTH] Tentativa de login: Usuario='%s'\n", req.Usuario)

	mutexUsuarios.Lock()
	usuarios, err := carregarUsuariosDoBanco()
	mutexUsuarios.Unlock()

	if err != nil {
		fmt.Printf("[AUTH] Erro ao carregar base de dados: %v\n", err)
		responderLogin(conn, false, "Erro interno no servidor", "", "")
		return
	}

	senhaCalculada := hashSimples(req.Senha)
	var usuarioLogado *UsuarioCadastrado

	for i := range usuarios {
		if usuarios[i].Usuario == req.Usuario && usuarios[i].Senha == senhaCalculada {
			usuarioLogado = &usuarios[i]
			break
		}
	}

	if usuarioLogado != nil {
		msg := fmt.Sprintf("Bem-vindo(a), %s!", usuarioLogado.Usuario)
		responderLogin(conn, true, msg, usuarioLogado.TipoUsuario, usuarioLogado.IDUsuario)
		fmt.Printf("[AUTH] Login aprovado: '%s' (%s)\n", usuarioLogado.Usuario, usuarioLogado.TipoUsuario)
	} else {
		responderLogin(conn, false, "Usuario ou senha incorretos", "", "")
		fmt.Printf("[AUTH] Login rejeitado para: '%s'\n", req.Usuario)
	}
}

func responderLogin(conn net.Conn, sucesso bool, mensagem string, tipoUsuario string, idUsuario string) {
	resp := protocolo.RespostaLogin{
		Tipo:        protocolo.TipoLoginRes,
		IDUsuario:   idUsuario,
		Sucesso:     sucesso,
		Mensagem:    mensagem,
		TipoUsuario: tipoUsuario,
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func CadastrarCliente(cliente *conexao.ClienteTCP, usuario string, senha string, perfil string) (bool, error) {
	req := protocolo.RegistroUsuario{
		Tipo:        protocolo.TipoCadastroReq,
		Usuario:     usuario,
		Senha:       senha,
		TipoUsuario: perfil,
	}

	if err := cliente.EnviarJSON(req); err != nil {
		return false, fmt.Errorf("erro ao transmitir dados de cadastro: %w", err)
	}

	limite := time.NewTimer(10 * time.Second)
	defer limite.Stop()

	for {
		select {
		case linha := <-cliente.MensagensRecebidas:
			var base protocolo.MensagemBase
			if err := json.Unmarshal(linha, &base); err != nil || base.Tipo != protocolo.TipoCadastroRes {
				continue // Ignora pacotes de outros tipos que porventura estejam no buffer
			}

			var resp protocolo.RespostaSimples
			if err := json.Unmarshal(linha, &resp); err != nil {
				return false, fmt.Errorf("erro ao decodificar resposta de cadastro: %w", err)
			}

			if !resp.Sucesso {
				return false, fmt.Errorf("%s", resp.Mensagem) // Retorna o erro exato do servidor
			}
			return true, nil

		case err := <-cliente.Erros:
			return false, fmt.Errorf("falha de rede durante o cadastro: %w", err)

		case <-limite.C:
			return false, fmt.Errorf("tempo limite esgotado aguardando o servidor")
		}
	}
}

func AutenticarCliente(cliente *conexao.ClienteTCP, usuario string, senha string, perfilEsperado string) (bool, error) {
	req := protocolo.CredencialLogin{
		Tipo:    protocolo.TipoLoginReq,
		Usuario: usuario,
		Senha:   senha,
	}

	if err := cliente.EnviarJSON(req); err != nil {
		return false, fmt.Errorf("erro ao transmitir credenciais: %w", err)
	}

	limite := time.NewTimer(10 * time.Second)
	defer limite.Stop()

	for {
		select {
		case linha := <-cliente.MensagensRecebidas:
			var base protocolo.MensagemBase
			if err := json.Unmarshal(linha, &base); err != nil || base.Tipo != protocolo.TipoLoginRes {
				continue // Impede que uma notificação caia aqui e crashe o login
			}

			var resp protocolo.RespostaLogin
			if err := json.Unmarshal(linha, &resp); err != nil {
				return false, fmt.Errorf("erro ao decodificar resposta de login: %w", err)
			}

			if !resp.Sucesso {
				return false, fmt.Errorf("%s", resp.Mensagem) // Agora passa a msg de "Usuário incorreto" pro passageiro.go
			}
			if resp.TipoUsuario != perfilEsperado {
				return false, fmt.Errorf("acesso negado: perfil '%s' nao autorizado (esperado '%s')", resp.TipoUsuario, perfilEsperado)
			}

			return true, nil

		case err := <-cliente.Erros:
			return false, fmt.Errorf("falha de rede durante a autenticacao: %w", err)

		case <-limite.C:
			return false, fmt.Errorf("tempo limite esgotado aguardando o servidor")
		}
	}
}
