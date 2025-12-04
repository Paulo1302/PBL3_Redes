# 🃏 Jogo de Cartas Multiplayer em Blockchain (IOTA)

Este projeto implementa um jogo de cartas multiplayer descentralizado utilizando a tecnologia **IOTA (Move)** para garantir a integridade dos ativos e a auditoria das partidas. A arquitetura segue o padrão de microsserviços orientados a eventos.

---

## 🏛️ Arquitetura do Sistema

O sistema é composto por 4 componentes principais que se comunicam via **NATS**:

- **IOTA Local Network**  
  A camada de ledger distribuído (Blockchain) onde residem os Smart Contracts, as Cartas (NFTs) e o registro de partidas.

- **Blockchain Server (TypeScript)**  
  Um "Worker" que atua como ponte segura. Ele escuta comandos do NATS, assina transações com a chave do administrador e interage com a IOTA.

- **Game Server (Go)**  
  O servidor central de lógica do jogo. Ele gerencia o matchmaking e solicita operações na blockchain (Mint, Log) via NATS.

- **Client (Go)**  
  Interface de linha de comando (CLI) para os jogadores.

---

## 🚀 Pré-requisitos

Certifique-se de ter instalado em sua máquina:

- Docker (Para rodar o servidor NATS)
- IOTA CLI (Para rodar a rede local)
- Node.js & NPM (Para o Blockchain Server)
- Go 1.22+ (Para o Game Server e Client)

---

## 🛠️ Passo a Passo para Execução

Siga a ordem exata abaixo para garantir que todos os serviços se conectem corretamente. Recomendo abrir 5 abas no terminal.

### 1. Iniciar a Infraestrutura (Terminais 1 e 2)

Precisamos subir o Message Broker e a Blockchain.

**Terminal 1 (NATS):** Execute o servidor de mensagens:

```bash
docker run -d --name nats-server -p 4222:4222 nats:latest
```

**Terminal 2 (Rede IOTA):** Este comando inicia a rede local, reseta o histórico (para limpar dados antigos) e ativa o faucet (distribuidor de moedas de teste).

```bash
iota start --with-faucet --force-regenesis
```

> ⚠️ Mantenha este terminal aberto e rodando durante todo o teste.

### 2. Configurar e Iniciar o Blockchain Server (Terminal 3)

Este serviço é responsável por publicar o contrato inteligente, gerar as chaves de administração e ouvir requisições do jogo.

Acesse a pasta:

```bash
cd src/blockchain_server
```

**A. Instalar Dependências (Apenas na primeira vez):**

```bash
npm install
```

**B. Deploy Automatizado (Obrigatório a cada reset da rede):**

Este script cria uma carteira de Admin, coloca saldo nela via faucet, compila o Smart Contract (`contracts/core.move`), publica na rede local e gera automaticamente o arquivo `.env` compartilhado com os IDs necessários.

```bash
npm run deploy
```

> Aguarde até ver a mensagem "✅ Deploy Sucesso!" e "💾 Arquivo .env atualizado com sucesso!"

**C. Iniciar o Worker:**

Agora que o ambiente está configurado, inicie o serviço.

```bash
npm run start
```

Você deve ver: "✅ Sistema pronto e escutando NATS."

### 3. Iniciar o Game Server (Terminal 4)

O servidor do jogo vai ler o endereço da carteira do `.env` gerado no passo anterior e conectar no NATS para solicitar a criação de cartas.

Acesse a pasta:

```bash
cd src/game_server
```

Execute o servidor:

```bash
go mod tidy  # (Apenas na primeira vez para baixar libs)
go run main.go
```

Você deve ver: "✅ Carteira da Loja Carregada: 0x..."

### 4. Iniciar o Cliente/Jogador (Terminal 5)

Agora você pode jogar.

Acesse a pasta:

```bash
cd src/client
```

Execute o cliente:

```bash
go run client.go
```

---

## 🎮 Como Jogar e Verificar a Blockchain

**Criar Usuário:** No Cliente, selecione a opção 3.
- **Verificação:** O Terminal 3 (TypeScript) mostrará a criação da carteira na IOTA e o Terminal 4 (Go) registrará o jogador.

**Login:** Faça login com o ID gerado (Opção 2).

**Abrir Pacote (Mint):** Selecione 1.
- Isso iniciará uma transação real. O jogador paga 1000 IOTA para a loja.
- O servidor solicita a criação (Mint) das cartas como NFTs na blockchain.
- **Verificação:** Copie o Digest que aparece no log do servidor.

**Batalha:** Abra um segundo terminal de cliente (Terminal 6), crie outro usuário e use a opção 4 em ambos para batalhar.
- Ao final, o resultado será gravado imutavelmente na blockchain.

---

## 🔍 Auditoria (Prova de Conceito)

Para provar que os ativos estão realmente na rede IOTA:

### 1. Via Terminal:

```bash
iota client objects <ENDERECO_DO_JOGADOR>
```

Procure na lista por objetos onde o `ObjectType` contenha `...::core::MonsterCard`.

### 2. Via Explorer Visual:

Acesse [Explorer IOTA Rebased](https://explorer.rebased.iota.org/).
- Mude a rede para Local (Custom RPC: `http://127.0.0.1:9000`).
- Busque pelo endereço do jogador ou pelo Digest da transação que apareceu no terminal.

---

## ⚠️ Solução de Problemas Comuns

- **Erro "Connection refused" no Go/TS:** Verifique se o Docker do NATS está rodando (Terminal 1).

- **Erro "Code -32002" ou "Object not found":** Você provavelmente reiniciou a rede IOTA (Terminal 2) mas esqueceu de rodar `npm run deploy` novamente no Terminal 3. As IDs mudam a cada reinício.

- **Saldo Insuficiente:** O sistema possui recarga automática (ensureFunds), mas se falhar, rode `iota client faucet` no terminal.
