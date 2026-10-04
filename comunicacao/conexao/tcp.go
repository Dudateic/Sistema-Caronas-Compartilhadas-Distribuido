package conexao

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
)

type ClienteTCP struct {
	endereco string
	conn     net.Conn
	mu       sync.Mutex

	MensagensRecebidas chan []byte
	Erros              chan error

	filaEnvio   chan []byte
	canalParada chan struct{}
	encerrado   bool
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
		endereco:           endereco,
		conn:               conn,
		MensagensRecebidas: make(chan []byte, 50),
		Erros:              make(chan error, 10),
		filaEnvio:          make(chan []byte, 50),
		canalParada:        make(chan struct{}),
	}

	go cliente.loopLeitura()
	go cliente.loopEnvio()

	return cliente, nil
}

func (cliente *ClienteTCP) loopLeitura() {
	leitor := bufio.NewReader(cliente.conn)

	for {
		select {
		case <-cliente.canalParada:
			return
		default:
			linha, err := leitor.ReadBytes('\n')
			if err != nil {
				select {
				case <-cliente.canalParada:
					return
				default:
					cliente.Erros <- fmt.Errorf("desconexao detetada na leitura: %w", err)
					return
				}
			}

			if len(linha) > 0 {
				cliente.MensagensRecebidas <- linha
			}
		}
	}
}


func (cliente *ClienteTCP) loopEnvio() {
	for {
		select {
		case <-cliente.canalParada:
			return
		case dados, ok := <-cliente.filaEnvio:
			if !ok {
				return
			}
			cliente.mu.Lock()
			_, err := cliente.conn.Write(dados)
			cliente.mu.Unlock()

			if err != nil {
				cliente.Erros <- fmt.Errorf("falha ao escrever no socket: %w", err)
			}
		}
	}
}


func (cliente *ClienteTCP) EnviarJSON(dados any) error {
	bytesDados, err := json.Marshal(dados)
	if err != nil {
		return fmt.Errorf("erro na serializacao json: %w", err)
	}

	bytesDados = append(bytesDados, '\n')

	select {
	case <-cliente.canalParada:
		return fmt.Errorf("cliente fechado, impossivel enviar")
	case cliente.filaEnvio <- bytesDados:
		return nil
	}
}


func (cliente *ClienteTCP) Fechar() error {
	cliente.mu.Lock()
	defer cliente.mu.Unlock()

	if cliente.encerrado {
		return nil
	}
	cliente.encerrado = true

	close(cliente.canalParada)


	var err error
	if cliente.conn != nil {
		err = cliente.conn.Close()
	}

	return err
}