package tests

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/cliente_servidor"
	"Sistema-Caronas-Compartilhadas-Distribuido/componentes/servicos"
)

func ouvir(t *testing.T, tratar func(net.Conn)) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go tratar(c)
		}
	}()
	return l.Addr().String()
}

func TestConectaQuandoServidorResponde(t *testing.T) {
	addr := ouvir(t, func(c net.Conn) {
		defer c.Close()
		l := bufio.NewReader(c)
		for {
			linha, err := l.ReadString('\n')
			if err != nil {
				return
			}
			var b struct {
				Tipo string `json:"tipo"`
			}
			_ = json.Unmarshal([]byte(linha), &b)
			if b.Tipo == cliente_servidor.TipoPingReq {
				servicos.ProcessarPing(c)
			}
		}
	})
	cli, err := cliente_servidor.ConectarTCP(addr, time.Second)
	if err != nil {
		t.Fatalf("deveria conectar: %v", err)
	}
	_ = cli.Fechar()
}

func TestNaoConectaSeAceitaEFecha(t *testing.T) {
	addr := ouvir(t, func(c net.Conn) { _ = c.Close() })
	if cli, err := cliente_servidor.ConectarTCP(addr, time.Second); err == nil {
		_ = cli.Fechar()
		t.Fatal("nao deveria dar como conectado: o outro lado fechou a conexao")
	}
}

func TestNaoConectaSeNaoResponde(t *testing.T) {
	addr := ouvir(t, func(c net.Conn) { time.Sleep(5 * time.Second); _ = c.Close() })
	inicio := time.Now()
	cli, err := cliente_servidor.ConectarTCP(addr, 400*time.Millisecond)
	if err == nil {
		_ = cli.Fechar()
		t.Fatal("nao deveria dar como conectado: ninguem respondeu ao handshake")
	}
	if !strings.Contains(err.Error(), "handshake") {
		t.Fatalf("erro deveria mencionar o handshake: %v", err)
	}
	if time.Since(inicio) > 2*time.Second {
		t.Fatalf("demorou demais para desistir: %s", time.Since(inicio))
	}
}

func TestNaoConectaSeNaoHaServidor(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	_ = l.Close()
	if cli, err := cliente_servidor.ConectarTCP(addr, 500*time.Millisecond); err == nil {
		_ = cli.Fechar()
		t.Fatal("nao deveria conectar")
	}
}
