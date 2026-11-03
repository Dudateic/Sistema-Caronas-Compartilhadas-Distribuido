# Uso: go build -o bin/servidor ./aplicacao/servidor && python3 tests/e2e/cancelamento.py ./bin/servidor
# Verifica: cancelamento de carona pelo motorista devolve os assentos nas outras empresas e o aviso
# a uma empresa fora do ar e guardado e reenviado quando ela volta.
import socket,json,sys,time,subprocess,os,shutil,signal
S=sys.argv[1]; R=sys.argv[2] if len(sys.argv)>2 else "/tmp/vaijunto-e2e"
shutil.rmtree(R,ignore_errors=True)
CFG={"A":(6060,9091,"B=http://localhost:9092,C=http://localhost:9093"),
     "B":(7070,9092,"A=http://localhost:9091,C=http://localhost:9093"),
     "C":(8080,9093,"A=http://localhost:9091,B=http://localhost:9092")}
procs={}
def start(n):
    t,h,p=CFG[n]; os.makedirs(f"{R}/{n}",exist_ok=True)
    procs[n]=subprocess.Popen([S,"-empresa="+n,f"-tcp=127.0.0.1:{t}",f"-http=127.0.0.1:{h}",f"-dados={R}/{n}","-pares="+p],stdout=open(f"{R}/{n}.log","a"),stderr=subprocess.STDOUT)
    time.sleep(0.8)
def stop(n): procs[n].kill(); procs[n].wait(); time.sleep(0.3)
def call(n,obj):
    s=socket.create_connection(("127.0.0.1",CFG[n][0]),3); f=s.makefile("rwb")
    f.write((json.dumps(obj)+"\n").encode()); f.flush(); r=json.loads(f.readline()); s.close(); return r
def seats(n): return [(t["origem"]+">"+t["destino"],t["passageiros"],t["assentos_livres"]) for c in json.load(open(f"{R}/{n}/dados.json")) for t in c["trechos"]]
def setup(p):
    call("A",{"tipo":"publicar_carona_requisicao","motorista":"ma","data":"2030-01-01","horario":"08:00","rota":["SSA","FSA"],"assentos_totais":1,"preco_por_trecho":5})
    call("B",{"tipo":"publicar_carona_requisicao","motorista":"mb","data":"2030-01-01","horario":"09:00","rota":["FSA","VCA"],"assentos_totais":1,"preco_por_trecho":5})
    it=call("C",{"tipo":"buscar_itinerarios_requisicao","origem":"SSA","destino":"VCA","data":"2030-01-01"})["itinerarios"][0]
    return call("C",{"tipo":"reservar_requisicao","passageiro":p,"itinerario":it})
ok=True
def check(nome,cond):
    global ok; print(("OK   " if cond else "FALHA"),nome); ok&=bool(cond)
for n in "ABC": start(n)

r=setup("p1"); check("reserva criada",r["sucesso"])
call("A",{"tipo":"cancelar_carona_requisicao","carona_id":1,"motorista":"ma"}); time.sleep(1.5)
check("1) assento em B devolvido apos motorista de A cancelar", seats("B")[0][2]==1 and seats("B")[0][1]==[])
check("1) reserva removida em C", not call("C",{"tipo":"consultar_reservas_requisicao","passageiro":"p1"}).get("reservas"))

call("B",{"tipo":"cancelar_carona_requisicao","carona_id":1,"motorista":"mb"}) if False else None
shutil.rmtree(R+"/A"); shutil.rmtree(R+"/B"); stop("A"); stop("B"); start("A"); start("B")
r=setup("p2"); check("2) reserva criada",r["sucesso"])
stop("B")
c=call("C",{"tipo":"cancelar_reserva_requisicao","reserva_id":r["reserva_id"],"passageiro":"p2"}); print("     msg:",c["mensagem"][:80])
check("2) aviso guardado no disco de C", os.path.exists(f"{R}/C/notificacoes_pendentes.json") and len(json.load(open(f"{R}/C/notificacoes_pendentes.json")))>0)
start("B"); time.sleep(4)
check("2) B liberou o assento depois de voltar", seats("B")[0][1]==[] and seats("B")[0][2]==1)
check("2) fila de C esvaziada", len(json.load(open(f"{R}/C/notificacoes_pendentes.json")))==0)

shutil.rmtree(R+"/A"); shutil.rmtree(R+"/B"); stop("A"); stop("B"); start("A"); start("B")
r=setup("p3"); check("3) reserva criada",r["sucesso"])
stop("C")
call("A",{"tipo":"cancelar_carona_requisicao","carona_id":1,"motorista":"ma"}); time.sleep(0.5)
check("3) B ainda ocupado enquanto C esta fora", seats("B")[0][1]==["p3"])
start("C"); time.sleep(4)
check("3) apos C voltar: B devolveu o assento", seats("B")[0][1]==[] and seats("B")[0][2]==1)
check("3) apos C voltar: reserva removida", not call("C",{"tipo":"consultar_reservas_requisicao","passageiro":"p3"}).get("reservas"))
for n in "ABC":
    try: stop(n)
    except Exception: pass
print("RESULTADO:", "TUDO OK" if ok else "HA FALHAS")
