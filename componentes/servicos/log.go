package service

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var mutexLog sync.Mutex

const ArquivoLog = "servidor.log"

func RegistrarLog(conn net.Conn, tipo string, _ string) {
	mutexLog.Lock()
	defer mutexLog.Unlock()

	_ = os.MkdirAll(DiretorioDados, 0755)
	f, err := os.OpenFile(filepath.Join(DiretorioDados, ArquivoLog), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s | %s | %s\n", time.Now().Format("2006-01-02 15:04:05"), conn.RemoteAddr(), tipo)
}
