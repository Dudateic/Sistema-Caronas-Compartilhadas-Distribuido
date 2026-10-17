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

	return cliente, nil
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
