package servicos

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/modelos"
)

var (
	ErrUsuarioJaExiste      = errors.New("login ja cadastrado")
	ErrCredenciaisInvalidas = errors.New("login ou senha incorretos")
)

type GerenciadorUsuarios struct {
	caminhoPersistencia string
	mu                  sync.RWMutex
	usuarios            map[string]modelos.Usuario // login -> Usuario
}

func NovoGerenciadorUsuarios(diretorioDados string) *GerenciadorUsuarios {
	caminho := filepath.Join(diretorioDados, "usuarios.json")
	gu := &GerenciadorUsuarios{
		caminhoPersistencia: caminho,
		usuarios:            make(map[string]modelos.Usuario),
	}
	_ = gu.carregarDoDisco()
	return gu
}

func (gu *GerenciadorUsuarios) Cadastrar(nome, login, senha string, tipo modelos.TipoUsuario) error {
	gu.mu.Lock()
	defer gu.mu.Unlock()

	if _, existe := gu.usuarios[login]; existe {
		return ErrUsuarioJaExiste
	}

	gu.usuarios[login] = modelos.Usuario{
		ID:       fmt.Sprintf("usr_%d", time.Now().UnixNano()),
		Nome:     nome,
		Login:    login,
		Senha:    senha,
		Tipo:     tipo,
		CriadoEm: time.Now(),
	}

	return gu.salvarEmDisco()
}

func (gu *GerenciadorUsuarios) Autenticar(login, senha string) (*modelos.SessaoUsuario, error) {
	gu.mu.RLock()
	defer gu.mu.RUnlock()

	usuario, existe := gu.usuarios[login]
	if !existe || usuario.Senha != senha {
		return nil, ErrCredenciaisInvalidas
	}

	return &modelos.SessaoUsuario{
		IDUsuario:  usuario.ID,
		Nome:       usuario.Nome,
		Tipo:       usuario.Tipo,
		IniciadaEm: time.Now(),
	}, nil
}
