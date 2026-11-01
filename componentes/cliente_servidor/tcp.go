package cliente_servidor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	EnderecoPadrao       = ":8081"
	TimeoutConexaoPadrao = 5 * time.Second
	TimeoutEscritaPadrao = 3 * time.Second
)

type ClienteTCP struct {
	conn net.Conn
	mu   sync.Mutex

	MensagensRecebidas chan []byte
	Erros              chan error
	canalParada        chan struct{}
	fecharOnce         sync.Once
}

func ConectarTCP(endereco string, timeout ...time.Duration) (*ClienteTCP, error) {
	if endereco == "" {
		endereco = EnderecoPadrao
	}

	limite := TimeoutConexaoPadrao
	if len(timeout) > 0 && timeout[0] > 0 {
		limite = timeout[0]
	}

	conn, err := net.DialTimeout("tcp", endereco, limite)
	if err != nil {
		return nil, fmt.Errorf("falha ao conectar a %s: %w", endereco, err)
	}

	cliente := &ClienteTCP{
		conn:               conn,
		MensagensRecebidas: make(chan []byte, 100),
		Erros:              make(chan error, 10),
		canalParada:        make(chan struct{}),
	}

	go cliente.loopLeitura()

	// Abrir o socket não prova que há um servidor VaiJunto do outro lado: um proxy do Docker,
	// um firewall ou um servidor travado aceitam a conexão TCP e não respondem nada.
	// Só consideramos "conectado" depois de o servidor responder a um handshake.
	if err := cliente.handshake(limite); err != nil {
		_ = cliente.Fechar()
		return nil, fmt.Errorf("falha ao conectar a %s: %w", endereco, err)
	}

	return cliente, nil
}

// handshake envia um ping e espera a resposta do servidor.
func (c *ClienteTCP) handshake(limite time.Duration) error {
	if limite > 3*time.Second {
		limite = 3 * time.Second
	}
	if err := c.EnviarJSON(map[string]any{"tipo": TipoPingReq}); err != nil {
		return fmt.Errorf("servidor nao aceitou o handshake: %w", err)
	}

	prazo := time.NewTimer(limite)
	defer prazo.Stop()
	for {
		select {
		case linha := <-c.MensagensRecebidas:
			var base struct {
				Tipo string `json:"tipo"`
			}
			if json.Unmarshal(linha, &base) == nil && base.Tipo == TipoPingRes {
				return nil
			}
		case err := <-c.Erros:
			return fmt.Errorf("a conexao foi encerrada pelo outro lado: %w", err)
		case <-prazo.C:
			return fmt.Errorf("o servidor nao respondeu ao handshake em %s (porta aberta, mas sem servidor VaiJunto respondendo)", limite)
		}
	}
}

func (c *ClienteTCP) EnviarJSON(dados any) error {
	bytesDados, err := json.Marshal(dados)
	if err != nil {
		return fmt.Errorf("erro na serializacao json: %w", err)
	}
	bytesDados = append(bytesDados, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()

	_ = c.conn.SetWriteDeadline(time.Now().Add(TimeoutEscritaPadrao))
	_, err = c.conn.Write(bytesDados)
	return err
}

func (c *ClienteTCP) loopLeitura() {
	leitor := bufio.NewReader(c.conn)

	for {
		linha, err := leitor.ReadBytes('\n')
		if err != nil {
			select {
			case <-c.canalParada:
				return
			default:
				c.Erros <- fmt.Errorf("desconexao detectada: %w", err)
				_ = c.Fechar()
				return
			}
		}

		if len(linha) > 0 {
			select {
			case c.MensagensRecebidas <- linha:
			case <-c.canalParada:
				return
			}
		}
	}
}

func (c *ClienteTCP) Fechar() error {
	var err error
	c.fecharOnce.Do(func() {
		close(c.canalParada)
		if c.conn != nil {
			err = c.conn.Close()
		}
	})
	return err
}
