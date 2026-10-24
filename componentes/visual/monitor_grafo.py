import json
import os
import requests
import networkx as nx
import matplotlib.pyplot as plt
from matplotlib.animation import FuncAnimation

SERVIDORES = [
    {
        "nome": "Empresa A",
        "url": "http://127.0.0.1:9091/api/v1/trechos",
        "fallback_json": "dados/empresa_A/dados.json",
        "cor_vertice": "#4A90E2",
    },
    {
        "nome": "Empresa B",
        "url": "http://127.0.0.1:9092/api/v1/trechos",
        "fallback_json": "dados/empresa_B/dados.json",
        "cor_vertice": "#50E3C2",
    },
    {
        "nome": "Empresa C",
        "url": "http://127.0.0.1:9093/api/v1/trechos",
        "fallback_json": "dados/empresa_C/dados.json",
        "cor_vertice": "#F5A623",
    },
]

def obter_trechos(config):
    """Tenta obter os trechos ativos via API REST; se falhar, lê do arquivo JSON."""
    try:
        resp = requests.get(config["url"], timeout=0.8)
        if resp.status_code == 200:
            dados = resp.json()
            if isinstance(dados, list):
                return [
                    (
                        t["origem"].upper(),
                        t["destino"].upper(),
                        t.get("assentos_livres", 1),
                        t.get("preco", 0.0),
                    )
                    for t in dados
                    if t.get("assentos_livres", 1) > 0
                ]
    except Exception:
        pass

    caminho = config["fallback_json"]
    if os.path.exists(caminho):
        try:
            with open(caminho, "r", encoding="utf-8") as f:
                caronas = json.load(f)
                arestas = []
                for carona in caronas:
                    for t in carona.get("trechos", []):
                        vagas = t.get("assentos_livres", 0)
                        if vagas > 0:
                            arestas.append(
                                (
                                    t["origem"].upper(),
                                    t["destino"].upper(),
                                    vagas,
                                    t.get("preco", 0.0),
                                )
                            )
                return arestas
        except Exception:
            pass

    return []

fig, axes = plt.subplots(1, 3, figsize=(18, 6))
fig.canvas.manager.set_window_title("Consórcio VaiJunto - Topologia de Grafos em Tempo Real")

posicoes_fixas = {}

def atualizar(frame):
    for ax, cfg in zip(axes, SERVIDORES):
        ax.clear()
        nome = cfg["nome"]
        trechos = obter_trechos(cfg)

        G = nx.DiGraph()

        arestas_agrupadas = {}
        for u, v, vagas, preco in trechos:
            chave = (u, v)
            if chave not in arestas_agrupadas:
                arestas_agrupadas[chave] = {"vagas": 0, "preco": preco}
            arestas_agrupadas[chave]["vagas"] += vagas

        for (u, v), info in arestas_agrupadas.items():
            G.add_edge(u, v, vagas=info["vagas"], preco=info["preco"])

        if len(G.nodes) == 0:
            ax.set_title(f"{nome} (Sem Caronas Ativas)", fontsize=13, fontweight="bold", color="#777")
            ax.axis("off")
            continue

        for node in G.nodes():
            if node not in posicoes_fixas:
                novas_pos = nx.spring_layout(G, seed=42, k=1.2)
                posicoes_fixas.update(novas_pos)
                break

        pos = {n: posicoes_fixas.get(n, (0, 0)) for n in G.nodes()}

        nx.draw_networkx_nodes(
            G,
            pos,
            ax=ax,
            node_color=cfg["cor_vertice"],
            node_size=1800,
            edgecolors="#333333",
            linewidths=1.5,
        )

        nx.draw_networkx_labels(
            G,
            pos,
            ax=ax,
            font_size=11,
            font_weight="bold",
            font_color="#FFFFFF" if cfg["cor_vertice"] != "#50E3C2" else "#1A1A1A",
        )

        nx.draw_networkx_edges(
            G,
            pos,
            ax=ax,
            arrows=True,
            arrowstyle="-|>",
            arrowsize=18,
            edge_color="#444444",
            width=2.0,
            connectionstyle="arc3,rad=0.12",
        )

        rotulos_arestas = {
            (u, v): f"{d['vagas']} vag.\nR$ {d['preco']:.0f}"
            for u, v, d in G.edges(data=True)
        }
        nx.draw_networkx_edge_labels(
            G,
            pos,
            edge_labels=rotulos_arestas,
            ax=ax,
            font_size=8,
            font_color="#990000",
            label_pos=0.45,
            rotate=False,
        )

        total_vagas = sum(d["vagas"] for _, _, d in G.edges(data=True))
        ax.set_title(
            f"{nome}\n({len(G.nodes)} nós | {len(G.edges)} rotas | {total_vagas} vagas)",
            fontsize=12,
            fontweight="bold",
        )
        ax.axis("off")

    plt.tight_layout()

ani = FuncAnimation(fig, atualizar, interval=2000, cache_frame_data=False)

if __name__ == "__main__":
    plt.show()