package simulador

/*
O QUE ESTÁ FAZENDO:
1. Mantém uma conexão TCP persistente (keep-alive) aberta por trabalhador.
2. Serializa payloads JSON com delimitador de quebra de linha ('\n') para
   adequação ao protocolo de streaming do servidor.
3. Se a conexão cair ou expirar por timeout, realiza reconexão transparente
   com retry automático antes de computar erro.
4. Evita o esgotamento de portas efêmeras (TIME_WAIT socket exhaustion) no Linux.

RESULTADOS ESPERADOS:
- Falhas de conexão próximas a 0 mesmo com 10.000+ requisições em curto intervalo.
- Redução expressiva da latência total (eliminando o handshake SYN-ACK repetitivo).
- Fechamento gracioso de sockets ao término de cada rotina trabalhadora.
*/

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

type ClienteTCPPersistente struct {
	alvo       string
	timeoutReq time.Duration
	conn       net.Conn
	leitor     *bufio.Reader
}

func NovoClienteTCP(alvo string, timeoutReq time.Duration) *ClienteTCPPersistente {
	return &ClienteTCPPersistente{
		alvo:       alvo,
		timeoutReq: timeoutReq,
	}
}

func (c *ClienteTCPPersistente) conectarSeNecessario() error {
	if c.conn != nil {
		return nil
	}
	conn, err := net.DialTimeout("tcp", c.alvo, 3*time.Second)
	if err != nil {
		return err
	}
	c.conn = conn
	c.leitor = bufio.NewReader(conn)
	return nil
}

func (c *ClienteTCPPersistente) Fechar() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.leitor = nil
	}
}

func (c *ClienteTCPPersistente) Executar(req any, tipoEsperado string, destino any) error {
	for tentativa := 0; tentativa < 2; tentativa++ {
		if err := c.conectarSeNecessario(); err != nil {
			return err
		}

		dados, err := json.Marshal(req)
		if err != nil {
			return err
		}
		dados = append(dados, '\n')

		_ = c.conn.SetDeadline(time.Now().Add(c.timeoutReq))
		if _, err := c.conn.Write(dados); err != nil {
			c.Fechar()
			continue
		}

		for {
			linha, err := c.leitor.ReadBytes('\n')
			if err != nil {
				c.Fechar()
				break
			}

			var base struct {
				Tipo string `json:"tipo"`
			}
			if err := json.Unmarshal(linha, &base); err != nil {
				continue
			}

			if base.Tipo == tipoEsperado {
				return json.Unmarshal(linha, destino)
			}
		}
	}
	return fmt.Errorf("falha ao comunicar com nó %s", c.alvo)
}
