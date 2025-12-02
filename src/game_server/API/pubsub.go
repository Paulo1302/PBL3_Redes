package API

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

func SetupPS(s *Store) {
	nc, err := BrokerConnect()
	if err != nil {
		log.Println("NATS Connect Error:", err)
		return
	}

	go func() {
		for {
			htb := map[string]int64{"server_ping": time.Now().UnixMilli()}
			htb_json, _ := json.Marshal(htb)
			nc.Publish("topic.heartbeat", htb_json)
		}
	}()

	ReplyPing(nc)
	CreateAccount(nc, s)
	ClientLogin(nc, s)
	ClientOpenPack(nc, s)
	ClientSeeCards(nc, s)
	ClientJoinGameQueue(nc, s)
	ClientPlayCards(nc, s)
	
	//DEBUG
	// for range 10 {
	// 	fmt.Println(RequestCreateWallet(nc))
	// }
	// for range 3 {
	// 	nc.Request("topic.createAccount", nil, 30*time.Second)
	// }
	// for i := 1; i < 3; i++ {
	// 	fmt.Println("faucet: ", RequestFaucet(nc, s.players[i].Wallet))
	// }
	
	// fmt.Println("player1 antes: ", RequestBalance(nc, s.players[1].Wallet))
	// fmt.Println("player2 antes: ", RequestBalance(nc, s.players[2].Wallet))
	// time.Sleep(2 * time.Second)
	// RequestTransaction(nc,s.players[1].Wallet,s.players[2].Wallet, 1000)
	// time.Sleep(2 * time.Second)
	// fmt.Println("player1 depois: ", RequestBalance(nc, s.players[1].Wallet))
	// fmt.Println("player2 depois: ", RequestBalance(nc, s.players[2].Wallet))
	
}

func ReplyPing(nc *nats.Conn) {
	nc.Subscribe("topic.ping", func(m *nats.Msg) {
		var payload map[string]any
		json.Unmarshal(m.Data, &payload)

		payload["server_ping"] = time.Now().UnixMilli()

		data, _ := json.Marshal(payload)
		nc.Publish(m.Reply, data)
	})
}

func BrokerConnect() (*nats.Conn, error) {
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = "nats://localhost:4222"
	}

	opts := []nats.Option{
		nats.Name("Central-Server"),
		nats.Timeout(10 * time.Second),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(5),
	}
	return nats.Connect(url, opts...)
}

func CreateAccount(nc *nats.Conn, s *Store) {
	nc.Subscribe("topic.createAccount", func(m *nats.Msg) {
		playerID, err := s.CreatePlayer(nc)
		
		if err != nil {
			nc.Publish(m.Reply, []byte(`{"err":"ERROR_CREATING"}`))
			return
		}
		payload := map[string]any{
			"status":    "player created",
			"player_id": playerID,
			"is_leader": true,
		}
		data, _ := json.Marshal(payload)
		nc.Publish(m.Reply, data)
		fmt.Println("user id: ",playerID)
	})
}

func ClientLogin(nc *nats.Conn, s *Store) {
	nc.Subscribe("topic.login", func(msg *nats.Msg) {
		var payload map[string]any
		json.Unmarshal(msg.Data, &payload)

		s.mu.Lock()
		maxCount := s.count
		_, exists := s.players[int(payload["client_id"].(float64))]
		s.mu.Unlock()

		if int(payload["client_id"].(float64)) > maxCount || !exists {
			payload["err"] = "user not found"
			data, _ := json.Marshal(payload)
			nc.Publish(msg.Reply, data)
			return
		}

		resp := map[string]any{
			"result":    true,
			"client_id": payload["client_id"],
		}
		data, _ := json.Marshal(resp)
		nc.Publish(msg.Reply, data)
	})
}

func ClientOpenPack(nc *nats.Conn, s *Store) {
	nc.Subscribe("topic.openPack", func(m *nats.Msg) {
		var payload map[string]any
		json.Unmarshal(m.Data, &payload)

		cards, err := s.OpenPack(int(payload["client_id"].(float64)))
		if err != nil {
			nc.Publish(m.Reply, []byte(`{"err":"ERROR_OPENING"}`))
			return
		}

		response := map[string]any{
			"status":    "Pack opened",
			"result":    *cards,
			"is_leader": true,
		}
		data, _ := json.Marshal(response)
		nc.Publish(m.Reply, data)
	})
}

// Adicionada implementação faltante para evitar erro em SetupPS
func ClientSeeCards(nc *nats.Conn, s *Store) {
	nc.Subscribe("topic.seeCards", func(m *nats.Msg) {
		var payload map[string]any
		json.Unmarshal(m.Data, &payload)
		clientID := int(payload["client_id"].(float64))

		s.mu.Lock()
		player, exists := s.players[clientID]
		s.mu.Unlock()
		if !exists {
			nc.Publish(m.Reply, []byte(`{"err":"player not found"}`))
			return
		}

		resp := map[string]any{
			"result":    player.Cards,
			"is_leader": true,
		}
		data, _ := json.Marshal(resp)
		nc.Publish(m.Reply, data)
	})
}



func ClientJoinGameQueue(nc *nats.Conn, s *Store) {
	nc.Subscribe("topic.findMatch", func(m *nats.Msg) {
		var payload map[string]any
		json.Unmarshal(m.Data, &payload)

		_, err := s.JoinQueue(int(payload["client_id"].(float64)))
		if err != nil {
			nc.Publish(m.Reply, []byte(`{"err":"ERROR_JOINING"}`))
			return
		}

		respPayload := map[string]any{"status": "Added to queue", "is_leader": true}
		data, _ := json.Marshal(respPayload)
		nc.Publish(m.Reply, data)

		match, err := s.CreateMatch()
		if err != nil {
			fmt.Println("Match not started.")
			return
		}

		fmt.Println("Match started:", match.SelfId)
		for _, p := range []int{match.P1, match.P2} {
			resp := map[string]any{
				"client_id": p,
				"match":     match,
			}
			fmt.Println(resp)
			data, _ = json.Marshal(resp)
			nc.Publish("topic.matchmaking", data)
		}

	})
}

// Função auxiliar para enviar resultados (era chamada mas não existia)
func SendingGameResult(payload map[string]any, nc *nats.Conn) {
	data, _ := json.Marshal(payload)
	if nc != nil {
		// Publica no tópico que os clientes estão escutando (ex: game.server)
		nc.Publish("game.server", data)
		fmt.Println("Result sent:", payload)
	}
}

func ClientPlayCards(nc *nats.Conn, s *Store) {
	nc.Subscribe("game.client", func(m *nats.Msg) {
		fmt.Println("REQUEST PLAY CARDS")

		var payload map[string]any
		if err := json.Unmarshal(m.Data, &payload); err != nil {
			log.Println("Error unmarshalling payload:", err)
			return
		}

		gameID := payload["game"].(string)
		clientID := int(payload["client_id"].(float64))
		card := int(payload["card"].(float64))

		// Executa a jogada
		err := s.PlayCard(gameID, clientID, card)
		if err != nil {
			log.Println("Error executing PlayCard:", err)
			return
		}

		// Verifica o estado do jogo
		s.mu.Lock()
		match, exists := s.matchHistory[gameID]
		s.mu.Unlock()

		if !exists {
			return
		}

		// Se ambos jogaram, calcula o resultado
		if match.Card1 != 0 && match.Card2 != 0 {

			// --- CORREÇÃO AQUI: Declarando separadamente para evitar erro ---
			var response1 map[string]any
			var response2 map[string]any
			// ---------------------------------------------------------------

			if match.Card1 > match.Card2 {
				response1 = map[string]any{
					"client_id": match.P1,
					"result":    "win",
					"card":      match.Card2,
				}
				response2 = map[string]any{
					"client_id": match.P2,
					"result":    "lose",
					"card":      match.Card1,
				}
			} else {
				response1 = map[string]any{
					"client_id": match.P1,
					"result":    "lose",
					"card":      match.Card2,
				}
				response2 = map[string]any{
					"client_id": match.P2,
					"result":    "win",
					"card":      match.Card1,
				}
			}

			SendingGameResult(response1, nc)
			SendingGameResult(response2, nc)
		}
	})
}
