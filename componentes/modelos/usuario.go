package modelos

import "time"

// TipoUsuario categoriza o perfil de acesso no sistema
type TipoUsuario string

const (
	UsuarioMotorista  TipoUsuario = "motorista"
	UsuarioPassageiro TipoUsuario = "passageiro"
)

type Usuario struct {
	ID       string      `json:"id"`
	Nome     string      `json:"nome"`
	Login    string      `json:"login"`
	Senha    string      `json:"senha"`
	Tipo     TipoUsuario `json:"tipo"`
	CriadoEm time.Time   `json:"criado_em"`
}

type RequisicaoCadastro struct {
	Nome  string      `json:"nome"`
	Login string      `json:"login"`
	Senha string      `json:"senha"`
	Tipo  TipoUsuario `json:"tipo"`
}

type RequisicaoLogin struct {
	Login string `json:"login"`
	Senha string `json:"senha"`
}

type SessaoUsuario struct {
	IDUsuario   string      `json:"id_usuario"`
	Nome        string      `json:"nome"`
	Tipo        TipoUsuario `json:"tipo"`
	TokenSessao string      `json:"token_sessao,omitempty"`
	IniciadaEm  time.Time   `json:"iniciada_em"`
}
