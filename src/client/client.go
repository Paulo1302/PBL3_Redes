package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	// Alias "API" para usar as funções do pacote definido em src/client/API/pubsub.go
	API "client/API" 

	"github.com/nats-io/nats.go"
)

var conn *nats.Conn

func main() {
	var htb = time.Now().UnixMilli()

	// Conecta ao NATS usando a função da biblioteca API
	nc := API.BrokerConnect(0)
	if nc == nil {
		fmt.Println("❌ Falha ao conectar no NATS. Verifique se o servidor está rodando.")
		return
	}

	conn = nc
	defer nc.Close()

	// Inicia o Heartbeat em background
	go API.Heartbeat(nc, &htb)
	
	// Inicia o Menu do Usuário
	go userMenu(nc)

	// Loop principal para manter o programa vivo enquanto houver conexão
	for time.Now().UnixMilli()-htb < 5000 {
		time.Sleep(1 * time.Second)
	}
	fmt.Println("❌ Desconectado do servidor (Timeout no Heartbeat).")
}

func userMenu(nc *nats.Conn) {
	id := 0
	cardChan := make(chan int)
	gameResult := make(chan string)
	
	// Inicia o listener de eventos do jogo
	API.ManageGame2(nc, &id, cardChan, gameResult)
	
	reader := bufio.NewReader(os.Stdin)

	for {
		// Fase 1: Menu Inicial (Login/Criar)
		id = menuInicial(nc, reader)
		
		if id != 0 {
			// Fase 2: Menu Principal (Logado)
			sub := API.LoggedIn(nc, id) // Avisa ao servidor que este cliente está ativo
			menuPrincipal(nc, id, reader, cardChan, gameResult)
			sub.Unsubscribe()
		}
	}
}

func menuInicial(nc *nats.Conn, reader *bufio.Reader) int {
	for {
		fmt.Println("\n=== MENU INICIAL ===")
		fmt.Println("1 - Ping Servidor")
		fmt.Println("2 - Login")
		fmt.Println("3 - Criar Nova Conta")
		fmt.Print("> ")

		opt, _ := reader.ReadString('\n')
		opt = strings.TrimSpace(opt)

		switch opt {
		case "1":
			ping := API.RequestPing(nc)
			if ping == -1 {
				fmt.Println("⚠️ Servidor não respondeu.")
			} else {
				fmt.Printf("✅ Pong! Latência: %d ms\n", ping)
			}
		case "2":
			fmt.Print("Digite seu ID: ")
			text, _ := reader.ReadString('\n')
			text = strings.TrimSpace(text)
			id, err := strconv.Atoi(text)
			if err != nil || id <= 0 {
				fmt.Println("ID inválido.")
				continue
			}
			
			ok, err := API.RequestLogin(nc, id)
			if err != nil || !ok {
				fmt.Println("❌ Erro no login:", err)
			} else {
				fmt.Println("✅ Login bem-sucedido!")
				return id
			}
		case "3":
			userID := API.RequestCreateAccount(nc)
			if userID == 0 {
				fmt.Println("❌ Erro ao criar usuário.")
			} else {
				fmt.Printf("✅ Usuário criado! Seu ID é: %d\n", userID)
				return userID
			}
		default:
			fmt.Println("Opção inválida.")
		}
	}
}

func menuPrincipal(nc *nats.Conn, id int, reader *bufio.Reader, cardChan chan int, roundResult chan string) {
	var cards []API.CardDisplay 
	var err error

	for {
		fmt.Printf("\n=== MENU PRINCIPAL (Jogador %d) ===\n", id)
		fmt.Println("1 - Comprar Pacote (Mint NFT)")
		fmt.Println("2 - Ver Minhas Cartas (On-Chain)")
		fmt.Println("3 - 🎲 Troca Cega (Blind Trade)")
		fmt.Println("4 - Batalhar (Matchmaking)")
		fmt.Println("5 - 🔑 Ver Minhas Credenciais (ID/Chaves)")
		fmt.Println("6 - Logout")
		fmt.Print("> ")

		opt, _ := reader.ReadString('\n')
		opt = strings.TrimSpace(opt)

		switch opt {
		case "1":
			fmt.Println("⏳ Processando compra na Blockchain IOTA...")
			newValues, err := API.RequestOpenPack(nc, id)
			if err != nil {
				fmt.Println("❌ Erro na compra:", err)
			} else {
				fmt.Println("🎉 Sucesso! Cartas obtidas (Força):", newValues)
			}

		case "2":
			fmt.Println("🌐 Consultando Blockchain...")
			cards, err = API.RequestSeeCards(nc, id)
			if err != nil {
				fmt.Println("❌ Erro ao buscar cartas:", err)
			} else {
				fmt.Println("\n--- SUAS CARTAS ---")
				if len(cards) == 0 {
					fmt.Println("Nenhuma carta encontrada.")
				} else {
					for i, c := range cards {
						fmt.Printf("[%d] ID: %s | Força: %d\n", i+1, c.ID, c.Power)
					}
					fmt.Println("-------------------")
					fmt.Println("Dica: Copie o ID (0x...) para trocar.")
				}
			}

		case "3":
			fmt.Println("\n--- 🎲 TROCA CEGA (BLIND TRADE) ---")
			fmt.Print("Cole o ID da SUA Carta (Hex): ")
			myCard, _ := reader.ReadString('\n')
			myCard = strings.TrimSpace(myCard)

			if myCard == "" {
				fmt.Println("ID inválido.")
				continue
			}

			fmt.Println("⏳ Validando posse e entrando na fila...")
			err := API.JoinBlindTrade(nc, id, myCard)
			
			if err != nil {
				fmt.Println("❌ Erro:", err)
			} else {
				fmt.Println("✅ Você está na fila! Aguardando match com outro jogador...")
				// Função bloqueante que espera a notificação PUSH
				API.WaitForTradeResult(nc, id)
			}

		case "4":
			fmt.Println("🔍 Buscando partida...")
			// Atualiza cartas antes de jogar para garantir sincronia
			cards, _ = API.RequestSeeCards(nc, id)
			
			if len(cards) == 0 {
				fmt.Println("Você precisa de cartas para jogar!")
				continue
			}
			game, err := API.RequestFindMatch(nc, id)
			if err != nil {
				fmt.Println("❌ Erro no matchmaking:", err)
			} else {
				menuJogo(nc, id, cards, reader, cardChan, roundResult, game)
			}

		case "5":
			fmt.Println("🔐 Buscando credenciais no servidor...")
			creds, err := API.RequestCredentials(nc, id)
			if err != nil {
				fmt.Println("❌ Erro ao buscar credenciais:", err)
			} else {
				fmt.Println("\n--- 🕵️ SEUS DADOS SECRETOS ---")
				fmt.Printf("🆔 ID de Jogador: %d\n", id)
				fmt.Printf("gd Endereço (Address): %s\n", creds.Address)
				fmt.Printf("🔑 Chave Privada (Secret): %s\n", creds.Secret)
				fmt.Println("⚠️  ATENÇÃO: Não compartilhe sua chave privada!")
				fmt.Println("-------------------------------")
			}

		case "6":
			return // Sai do loop e volta pro Menu Inicial

		default:
			fmt.Println("Opção inválida.")
		}
	}
}

func menuJogo(nc *nats.Conn, id int, cards []API.CardDisplay, reader *bufio.Reader, cardChan chan int, gameResult chan string, game string) {
	fmt.Println("\n⚔️ PARTIDA ENCONTRADA! ⚔️")
	fmt.Print("Suas cartas (Força): ")
	for _, c := range cards {
		fmt.Printf("[%d] ", c.Power)
	}
	fmt.Println()
	
	// Inicia heartbeat específico do jogo
	sub := API.ImAlive(conn, id)
	defer sub.Unsubscribe() 

	for {
		fmt.Print("Escolha a FORÇA da carta para jogar: ")
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)
		num, _ := strconv.Atoi(text)

		valid := false
		for _, c := range cards {
			if c.Power == num {
				valid = true
				break
			}
		}

		if !valid {
			fmt.Println("Você não possui uma carta com essa força.")
			continue
		}

		API.SendCards(nc, id, num, game)
		fmt.Println("Carta enviada! Aguardando oponente...")
		break
	}

	opCard := <-cardChan
	result := <-gameResult

	fmt.Printf("\nOponente jogou força: %d\n", opCard)
	
	switch result {
	case "win":
		fmt.Println("🏆 VITÓRIA! (Registrado na Blockchain)")
	case "lose":
		fmt.Println("💀 DERROTA. (Registrado na Blockchain)")
	case "draw":
		fmt.Println("🤝 EMPATE.")
	default:
		fmt.Println("⚠️ Erro na partida.")
	}
}