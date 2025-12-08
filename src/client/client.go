package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	// Alias "API" para usar as funções do pacote pubsub.go
	API "client/API" 

	"github.com/nats-io/nats.go"
)

var conn *nats.Conn

func main() {
	var htb = time.Now().UnixMilli()

	// CORREÇÃO: Usar API.BrokerConnect (nome do pacote importado)
	nc := API.BrokerConnect(0)
	if nc == nil {
		fmt.Println("❌ Falha ao conectar no NATS.")
		return
	}

	conn = nc
	defer nc.Close()

	// CORREÇÃO: Usar API.Heartbeat
	go API.Heartbeat(nc, &htb)
	go userMenu(nc)

	// Loop para manter vivo
	for time.Now().UnixMilli()-htb < 5000 {
		time.Sleep(1 * time.Second)
	}
	fmt.Println("Desconectado do servidor (Timeout).")
}

func userMenu(nc *nats.Conn) {
	id := 0
	cardChan := make(chan int)
	gameResult := make(chan string)
	
	API.ManageGame2(nc, &id, cardChan, gameResult)
	reader := bufio.NewReader(os.Stdin)

	for {
		id = menuInicial(nc, reader)
		if id != 0 {
			sub := API.LoggedIn(nc, id)
			menuPrincipal(nc, id, reader, cardChan, gameResult)
			sub.Unsubscribe()
		}
	}
}

func menuInicial(nc *nats.Conn, reader *bufio.Reader) int {
	for {
		fmt.Println("\n--- MENU INICIAL ---")
		fmt.Println("1 - Ping")
		fmt.Println("2 - Login")
		fmt.Println("3 - Criar usuário")
		fmt.Print("> ")

		opt, _ := reader.ReadString('\n')
		opt = strings.TrimSpace(opt)

		switch opt {
		case "1":
			ping := API.RequestPing(nc)
			if ping == -1 {
				fmt.Println("Erro no ping")
			} else {
				fmt.Println("Ping:", ping, "ms")
			}
		case "2":
			fmt.Print("ID: ")
			text, _ := reader.ReadString('\n')
			text = strings.TrimSpace(text)
			id, _ := strconv.Atoi(text)
			if id <= 0 {
				continue
			}
			ok, err := API.RequestLogin(nc, id)
			if err != nil || !ok {
				fmt.Println("Erro no login:", err)
			} else {
				fmt.Println("Login bem-sucedido!")
				return id
			}
		case "3":
			userID := API.RequestCreateAccount(nc)
			if userID == 0 {
				fmt.Println("Erro ao criar usuário")
			} else {
				fmt.Println("Usuário criado! ID:", userID)
				return userID
			}
		default:
			fmt.Println("Opção inválida")
		}
	}
}

func menuPrincipal(nc *nats.Conn, id int, reader *bufio.Reader, card chan(int), roundResult chan(string)) {
	var cards []API.CardDisplay 
	var err error

	for {
		fmt.Println("\n--- MENU PRINCIPAL ---")
		fmt.Println("1 - Abrir pacote (Comprar)")
		fmt.Println("2 - Ver cartas")
		fmt.Println("3 - 🎲 Troca Cega (Blind Trade)")
		fmt.Println("4 - Encontrar partida")
		fmt.Println("5 - Sair")
		fmt.Print("> ")

		opt, _ := reader.ReadString('\n')
		opt = strings.TrimSpace(opt)

		switch opt {
		case "1":
			fmt.Println("Processando compra na Blockchain...")
			newValues, err := API.RequestOpenPack(nc, id)
			if err != nil {
				fmt.Println("Erro na compra:", err)
			} else {
				fmt.Println("Parabéns! Cartas obtidas (Força):", newValues)
			}
		case "2":
			cards, err = API.RequestSeeCards(nc, id)
			if err != nil {
				fmt.Println("Erro ao buscar cartas:", err)
			} else {
				fmt.Println("\n--- SUAS CARTAS ---")
				for i, c := range cards {
					fmt.Printf("[%d] Power: %-4d | ID: %s\n", i+1, c.Power, c.ID)
				}
				fmt.Println("-------------------")
			}

		case "3":
			fmt.Println("\n--- 🎲 TROCA CEGA ---")
			fmt.Print("Cole o ID da SUA Carta (Hex): ")
			myCard, _ := reader.ReadString('\n')
			myCard = strings.TrimSpace(myCard)

			if myCard == "" {
				fmt.Println("ID inválido.")
				continue
			}

			fmt.Println("Entrando na fila...")
			err := API.JoinBlindTrade(nc, id, myCard)
			if err != nil {
				fmt.Println("Erro:", err)
			} else {
				fmt.Println("✅ Você está na fila! Aguarde notificação...")
				API.WaitForTradeResult(nc, id)
			}

		case "4":
			fmt.Println("Buscando partida...")
			cards, _ = API.RequestSeeCards(nc, id)
			if len(cards) == 0 {
				fmt.Println("Você não tem cartas.")
				continue
			}
			game, err := API.RequestFindMatch(nc, id)
			if err != nil {
				fmt.Println("Erro matchmaking:", err)
			} else {
				menuJogo(nc, id, cards, reader, card, roundResult, game)
			}
		case "5":
			return
		default:
			fmt.Println("Opção inválida")
		}
	}
}

func menuJogo(nc *nats.Conn, id int, cards []API.CardDisplay, reader *bufio.Reader, cardChan chan(int), gameResult chan(string), game string) {
	fmt.Println("\n--- PARTIDA ---")
	fmt.Print("Suas cartas: ")
	for _, c := range cards {
		fmt.Printf("[%d] ", c.Power)
	}
	fmt.Println()
	
	s := API.ImAlive(conn, id)
	defer s.Unsubscribe() 

	for {
		fmt.Print("Escolha a FORÇA da carta: ")
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
			fmt.Println("Carta inválida.")
			continue
		}

		API.SendCards(nc, id, num, game)
		break
	}

	fmt.Println("Aguardando oponente...")
	opCard := <-cardChan
	result := <-gameResult

	fmt.Printf("Oponente jogou: %d\n", opCard)
	
	if result == "win" {
		fmt.Println("🏆 VITÓRIA!")
	} else if result == "lose" {
		fmt.Println("💀 DERROTA.")
	} else {
		fmt.Println("⚠️ EMPATE/ERRO.")
	}
}