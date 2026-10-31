#!/usr/bin/env python3
import sys
import os
import time
import json
import socket
import matplotlib.pyplot as plt
from matplotlib.animation import FuncAnimation
import networkx as nx

# IP da máquina onde os servidores estão rodando
IP_HOST = sys.argv[1] if len(sys.argv) > 1 else os.getenv("HOST_SERVIDORES", "172.16.201.9")

SERVIDORES_TCP = {
    "A": (IP_HOST, 6060),
    "B": (IP_HOST, 7070),
    "C": (IP_HOST, 8080),
}

CORES_NODOS = {"A": "#10b981", "B": "#f59e0b", "C": "#ec4899"}
POSICOES = {"A": (-1.0, 0.5), "B": (1.0, 0.5), "C": (0.0, -0.8)}

fig, ax = plt.subplots(figsize=(11, 7.5))
fig.patch.set_facecolor('#0f172a')

ultimo_diag = {}

def consultar_via_tcp(host, porta):
    """Testa a conexão e tenta buscar o estado das caronas."""
    # 1. Teste de conectividade do socket TCP
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.settimeout(1.2)
        s.connect((host, porta))
    except Exception as e:
        return [], "OFFLINE", f"Falha socket: {e}"

    # 2. Envio da requisição de consulta (com framing \n)
    try:
        # Envia tentativa de consulta
        req = json.dumps({"tipo": "CONSULTAR_CARONAS_REQ"}) + "\n"
        s.sendall(req.encode("utf-8"))

        s.settimeout(1.0)
        buffer = ""
        while True:
            chunk = s.recv(4096).decode("utf-8")
            if not chunk:
                break
            buffer += chunk
            if "\n" in buffer:
                break
        s.close()

        # Parse das linhas JSON
        for linha in buffer.split("\n"):
            linha = linha.strip()
            if not linha:
                continue
            dados = json.loads(linha)
            # Se recebeu qualquer JSON estruturado do servidor
            caronas = dados.get("caronas") or dados.get("Caronas") or []
            return caronas, "ONLINE", "OK"

        return [], "ONLINE", "Conectado (sem caronas)"
    except Exception as e:
        # Se conectou mas a leitura expirou, o servidor AINDA ESTÁ ONLINE!
        try:
            s.close()
        except:
            pass
        return [], "ONLINE", f"Lendo dados: {type(e).__name__}"

def obter_dados():
    total_livres = {"A": 0, "B": 0, "C": 0}
    total_ocupados = {"A": 0, "B": 0, "C": 0}
    status_servidor = {}

    for emp, (host, porta) in SERVIDORES_TCP.items():
        caronas, status, diag = consultar_via_tcp(host, porta)
        status_servidor[emp] = status
        ultimo_diag[emp] = diag

        for c in caronas:
            trechos = c.get("Trechos") or c.get("trechos") or []
            for t in trechos:
                passag = t.get("Passageiros") or t.get("passageiros") or []
                livres = t.get("AssentosLivres") or t.get("assentos_livres") or 0
                total_livres[emp] += int(livres)
                total_ocupados[emp] += len(passag)

    return status_servidor, total_livres, total_ocupados

def atualizar(frame):
    ax.clear()
    ax.set_facecolor('#0f172a')

    status, livres, ocupados = obter_dados()

    G = nx.Graph()
    for s in SERVIDORES_TCP.keys():
        G.add_node(s)
    G.add_edge("A", "B")
    G.add_edge("B", "C")
    G.add_edge("A", "C")

    nx.draw_networkx_edges(G, POSICOES, ax=ax, edge_color="#38bdf8", width=2.5, style="dashed", alpha=0.6)

    node_colors = [CORES_NODOS[n] if status.get(n) == "ONLINE" else "#475569" for n in G.nodes()]
    nx.draw_networkx_nodes(G, POSICOES, ax=ax, node_color=node_colors, node_size=3200, edgecolors="#ffffff", linewidths=2)

    labels = {n: f"Empresa {n}\n({status.get(n, 'OFFLINE')})" for n in G.nodes()}
    nx.draw_networkx_labels(G, POSICOES, labels=labels, ax=ax, font_size=10, font_weight="bold", font_color="#ffffff")

    deslocamentos = {"A": (-1.0, 0.95), "B": (1.0, 0.95), "C": (0.0, -1.30)}
    for n, (x, y) in deslocamentos.items():
        txt = f"Vagas Livres: {livres[n]}\nPassageiros: {ocupados[n]}\n[{ultimo_diag.get(n, '')}]"
        ax.text(x, y, txt, color="#f8fafc", fontsize=9, ha="center", va="center",
                bbox=dict(boxstyle="round,pad=0.4", fc="#1e293b", ec=CORES_NODOS[n], lw=1.5))

    ax.text(0.0, 1.50, f"MONITORAMENTO AO VIVO DO CONSÓRCIO [TCP: {IP_HOST}]",
            color="#38bdf8", fontsize=13, fontweight="bold", ha="center")

    info_rodape = (
        f"Horário: {time.strftime('%H:%M:%S')}  |  "
        f"Total Vagas Livres: {sum(livres.values())}  |  "
        f"Total Passageiros: {sum(ocupados.values())}"
    )
    ax.text(0.0, -1.70, info_rodape, color="#94a3b8", fontsize=9.5, ha="center")

    ax.set_xlim(-1.6, 1.6)
    ax.set_ylim(-1.85, 1.65)
    ax.axis("off")

ani = FuncAnimation(fig, atualizar, interval=600, cache_frame_data=False)

if __name__ == "__main__":
    plt.show()