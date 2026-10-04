# TEC502 - TP02: Especificação do Problema
> **TEC502 - TP02 - Concorrência e Conectividade — Semestre 2026.2 | UEFS**


## 1. Objetivo

Implementar uma versão distribuída do VaiJunto, permitindo a realização de buscas e reservas atômicas de itinerários compostos por caronas disponibilizadas por diferentes empresas participantes de um consórcio.

Cada empresa opera de forma autônoma, gerenciando seu próprio servidor e base de dados local, realizando a comunicação com as demais exclusivamente por meio de uma **API REST**.


## 2. Servidores (Empresas do Consórcio)

O sistema deve ser composto por, no mínimo, **três servidores**, cada um representando uma empresa distinta.

Cada servidor é responsável por:
* Gerenciar e persistir as caronas disponibilizadas por sua respectiva empresa;
* Gerenciar os passageiros e motoristas vinculados localmente;
* Manter seu próprio armazenamento de dados local (sem banco compartilhado ou centralizado);
* Controlar a alocação e o bloqueio de assentos sob sua responsabilidade;
* Comunicar-se com os demais servidores do consórcio.

Cada servidor deve ser executado em um contêiner **Docker** isolado.


## 3. Clientes

O sistema dispõe de dois tipos de clientes:
* **Motorista:** Responsável pela publicação e pelo gerenciamento de suas caronas.
* **Passageiro:** Responsável pela realização de buscas e reservas de itinerários.

Cada cliente estabelece conexão **exclusivamente** com o servidor da sua própria empresa.


## 4. Comunicação Inter-Servidores

A comunicação entre os servidores das empresas parceiras deve ser realizada estritamente por meio de uma **API REST**.

A API deve especificar:
* Recursos disponibilizados;
* Métodos HTTP utilizados;
* Formatos das requisições e respostas;
* Códigos de retorno (*Status Codes*);
* Operações necessárias para a realização das reservas distribuídas.

A arquitetura não deve possuir servidor central nem coordenador fixo.


## 5. Reservas e Atomicidade

A confirmação de uma reserva deve possuir comportamento estritamente **atômico**.

Dessa forma, um itinerário deve ser confirmado integralmente ou não ser confirmado:
* Todos os trechos do itinerário devem ser reservados; ou
* Nenhum trecho deve permanecer reservado.

Essa propriedade deve ser mantida mesmo quando os trechos do itinerário estiverem distribuídos entre diferentes empresas parceiras.


## 6. Concorrência e Exclusão Mútua

O sistema deve assegurar a exclusão mútua sobre os assentos das caronas, impedindo que a mesma vaga seja confirmada para dois passageiros distintos.

Em situações de concorrência pela última vaga disponível de um trecho, o sistema deve adotar um critério de precedência bem definido que **não dependa da sincronização física dos relógios** dos servidores (ex.: relógios lógicos de Lamport ou identificadores determinísticos).


## 7. Tolerância a Falhas e Consistência

A indisponibilidade temporária de um servidor não deve:
* Resultar na confirmação parcial de um itinerário;
* Manter assentos bloqueados indefinidamente (*deadlocks* ou vazamento de recursos);
* Impedir a operação das demais empresas para caronas que não dependam da empresa indisponível.

Os demais servidores devem detectar a indisponibilidade por meio de **timeouts** (ou mecanismo equivalente) e continuar operando de forma segura.

Após a recuperação do servidor que falhou, o estado das reservas deve permanecer consistente em todo o consórcio.


## 8. Simulador de Carga

O sistema deve disponibilizar um simulador em linha de comando (sem interface gráfica) capaz de gerar automaticamente:
* Buscas simultâneas de rotas;
* Reservas concorrentes com alta contenção por assentos;
* Grande volume de requisições por segundo.

O simulador será utilizado para avaliar o comportamento e a robustez do sistema sob diferentes condições de carga.


## 9. Testes Automatizados

Devem ser implementados testes automatizados cobrindo cenários de carga e injeção de falhas, comprovando que:
* Nenhum assento seja confirmado para mais de um passageiro simultaneamente;
* Nenhum itinerário seja confirmado parcialmente;
* Nenhum assento permaneça bloqueado após a queda e posterior recuperação de um servidor.


## 10. Restrições Arquiteturais Obrigatórias

* Não utilizar servidor central;
* Não utilizar armazenamento compartilhado entre as empresas;
* Cada servidor deve manter exclusivamente os dados sob responsabilidade de sua respectiva empresa;
* A comunicação entre os servidores deve utilizar API REST;
* Os mecanismos de coordenação distribuída (exclusão mútua, atomicidade e recuperação de falhas) devem ser implementados integralmente pela própria aplicação;
* Não utilizar serviços externos de coordenação ou gerenciamento de transações (como ZooKeeper, etcd, Redis, Consul ou transações XA);
* Cada servidor deve ser executado em um contêiner Docker isolado.