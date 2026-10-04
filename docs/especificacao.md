# Especificação do Problema

> **TEC502 - TP02 - Concorrência e Conectividade - 2026.2 | UEFS**

## 1. Objetivo

Implementar uma versão distribuída do VaiJunto, permitindo a realização de buscas e reservas de itinerários compostos por caronas disponibilizadas por diferentes empresas do consórcio.

Cada empresa deverá manter seu próprio servidor e seus respectivos dados, realizando a comunicação com os demais servidores por meio de uma API REST.

## 2. Servidores

O sistema deverá ser composto por, no mínimo, três servidores, cada um representando uma empresa distinta do consórcio.

Cada servidor será responsável por:

* Manter as caronas disponibilizadas pela respectiva empresa;
* Gerenciar os passageiros e motoristas vinculados à empresa;
* Manter seu próprio armazenamento de dados;
* Comunicar-se com os demais servidores do consórcio;
* Controlar os assentos das caronas sob sua responsabilidade.

Cada servidor deverá ser executado em um contêiner Docker isolado.

## 3. Clientes

O sistema deverá disponibilizar dois tipos de clientes:

* **Motorista:** Responsável pela publicação e gerenciamento de caronas;
* **Passageiro:** Responsável pela realização de buscas e reservas de itinerários.

Cada cliente deverá estabelecer conexão exclusivamente com o servidor de sua própria empresa.

## 4. Comunicação

A comunicação entre os servidores das empresas deverá ser realizada por meio de uma **API REST**.

A API deverá especificar:

* Recursos disponibilizados;
* Métodos HTTP utilizados;
* Formatos das requisições;
* Formatos das respostas;
* Códigos de retorno;
* Operações necessárias para a realização de reservas distribuídas.

A arquitetura não deverá utilizar um servidor central ou um coordenador fixo.

## 5. Reservas

A confirmação de uma reserva deverá possuir comportamento **atômico**.

Dessa forma, um itinerário deverá ser confirmado integralmente ou não ser confirmado.

Consequentemente:

* Todos os trechos do itinerário deverão ser reservados; ou
* Nenhum dos trechos deverá permanecer reservado.

Essa propriedade deverá ser mantida mesmo quando os trechos do itinerário estiverem distribuídos entre diferentes empresas.

## 6. Concorrência

O sistema deverá garantir a exclusão mútua sobre os assentos das caronas, impedindo que um mesmo assento seja confirmado para dois passageiros distintos.

Em situações de concorrência pela última vaga disponível, o sistema deverá estabelecer um critério de precedência bem definido, que não dependa da sincronização dos relógios dos servidores.

## 7. Tolerância a Falhas

A indisponibilidade de um servidor não deverá:

* Resultar na confirmação parcial de um itinerário;
* Manter assentos bloqueados indefinidamente;
* Impedir a operação das demais empresas para caronas que não dependam do servidor indisponível.

Os demais servidores deverão detectar a indisponibilidade por meio de timeouts ou mecanismo equivalente e continuar operando de forma segura.

Após a recuperação do servidor, o estado das reservas deverá permanecer consistente em todo o consórcio.

## 8. Simulador

O sistema deverá possuir um simulador, sem interface gráfica, capaz de gerar automaticamente:

* Buscas simultâneas;
* Reservas simultâneas;
* Um grande volume de requisições.

O simulador deverá ser utilizado para avaliar o comportamento do sistema sob diferentes condições de carga.

## 9. Testes

Deverão ser implementados testes automatizados, incluindo cenários de carga e injeção de falhas.

Os testes deverão verificar, no mínimo, que:

* Nenhum assento seja confirmado para mais de um passageiro;
* Nenhum itinerário seja confirmado parcialmente;
* Nenhum assento permaneça bloqueado após a queda e posterior recuperação de um servidor.

## 10. Restrições

A implementação deverá respeitar as seguintes restrições:

* Não utilizar um servidor central;
* Não utilizar armazenamento compartilhado entre as empresas;
* Cada servidor deverá manter exclusivamente os dados sob responsabilidade de sua respectiva empresa;
* A comunicação entre os servidores deverá utilizar uma API REST;
* Os mecanismos de coordenação distribuída deverão ser implementados pela própria aplicação;
* Não utilizar serviços externos de coordenação ou gerenciamento de transações, como ZooKeeper, etcd, Redis, Consul ou transações XA;
* Cada servidor deverá ser executado em um contêiner Docker isolado.
