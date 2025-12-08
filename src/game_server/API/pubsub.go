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

	// Loop contínuo que envia um heartbeat para os clientes,
	// garantindo que quem estiver conectado saiba que o servidor está ativo.
	// A pausa de 1 segundo evita que o NATS marque o cliente como slow consumer.
	go func() {
		for {
			htb := map[string]int64{"server_ping": time.Now().UnixMilli()}
			htb_json, _ := json.Marshal(htb)
			nc.Publish("topic.heartbeat", htb_json)
			time.Sleep(1 * time.Second)
		}
	}()

	// Registro de todos os handlers que tratam as operações do jogo.
	ReplyPing(nc)
	CreateAccount(nc, s)
	ClientLogin(nc, s)
	ClientOpenPack(nc, s)
	ClientSeeCards(nc, s)
	ClientJoinGameQueue(nc, s)
	ClientPlayCards(nc, s)
	ClientJoinBlindTrade(nc, s)
	ClientGetCredentials(nc, s)
}

func ReplyPing(nc *nats.Conn) {
	// Responde automaticamente qualquer ping enviado por um cliente,
	// retornando o timestamp do servidor.
	nc.Subscribe("topic.ping", func(m *nats.Msg) {
		var payload map[string]any
		json.Unmarshal(m.Data, &payload)
		payload["server_ping"] = time.Now().UnixMilli()
		data, _ := json.Marshal(payload)
		nc.Publish(m.Reply, data)
	})
}

func BrokerConnect() (*nats.Conn, error) {
	// Define a URL do servidor NATS; caso não exista no ambiente, usa localhost.
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = "nats://localhost:4222"
	}
	// Configura opções de timeout, nome, e tentativas de reconexão.
	opts := []nats.Option{
		nats.Name("Central-Server"),
		nats.Timeout(10 * time.Second),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(5),
	}
	return nats.Connect(url, opts...)
}

func CreateAccount(nc *nats.Conn, s *Store) {
	// Cria um jogador novo e envia o ID ao cliente.
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
		fmt.Println("user id: ", playerID)
	})
}

func ClientLogin(nc *nats.Conn, s *Store) {
	// Verifica se um ID enviado pelo cliente corresponde a um jogador existente.
	nc.Subscribe("topic.login", func(msg *nats.Msg) {
		var payload map[string]any
		json.Unmarshal(msg.Data, &payload)

		s.mu.Lock()
		maxCount := s.count
		id := int(payload["client_id"].(float64))
		_, exists := s.players[id]
		s.mu.Unlock()

		if id > maxCount || !exists {
			payload["err"] = "user not found"
			data, _ := json.Marshal(payload)
			nc.Publish(msg.Reply, data)
			return
		}

		resp := map[string]any{"result": true, "client_id": id}
		data, _ := json.Marshal(resp)
		nc.Publish(msg.Reply, data)
	})
}

func ClientOpenPack(nc *nats.Conn, s *Store) {
	// Solicita ao Store que abra um pacote de cartas para o jogador,
	// enviando o resultado ao cliente.
	nc.Subscribe("topic.openPack", func(m *nats.Msg) {
		var payload map[string]any
		json.Unmarshal(m.Data, &payload)

		cards, err := s.OpenPack(nc, int(payload["client_id"].(float64)))
		if err != nil {
			resp := map[string]any{"err": err.Error()}
			data, _ := json.Marshal(resp)
			nc.Publish(m.Reply, data)
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

func ClientSeeCards(nc *nats.Conn, s *Store) {
	// Recupera as cartas do jogador diretamente da blockchain,
	// garantindo consistência entre on-chain e cache local.
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

		fmt.Printf("🌐 Consultando cartas on-chain para Jogador %d (%s)...\n", clientID, player.Wallet.Address)

		// Consulta o indexer para buscar as cartas reais registradas na blockchain.
		chainCards, err := RequestGetCardsFromChain(nc, player.Wallet.Address)

		if err != nil {
			errMsg := fmt.Sprintf(`{"err":"Falha ao consultar blockchain: %v"}`, err)
			nc.Publish(m.Reply, []byte(errMsg))
			return
		}

		// Atualiza o cache local de cartas baseado na verdade on-chain.
		s.mu.Lock()
		p, ok := s.players[clientID]
		if ok {
			newMap := make(map[string]int)
			for _, c := range chainCards {
				newMap[c.ID] = c.Power
			}
			p.Cards = newMap
			s.players[clientID] = p
		}
		s.mu.Unlock()

		resp := map[string]any{
			"result":    chainCards,
			"is_leader": true,
		}
		data, _ := json.Marshal(resp)
		nc.Publish(m.Reply, data)
	})
}

func ClientJoinGameQueue(nc *nats.Conn, s *Store) {
	// Adiciona o jogador à fila de matchmaking. Quando houver 2 players, inicia o duelo.
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

		// Notifica ambos os players envolvidos.
		for _, p := range []int{match.P1, match.P2} {
			resp := map[string]any{"client_id": p, "match": match}
			data, _ = json.Marshal(resp)
			nc.Publish("topic.matchmaking", data)
		}
	})
}

func SendingGameResult(payload map[string]any, nc *nats.Conn) {
	// Envia o resultado da rodada para o tópico do servidor.
	data, _ := json.Marshal(payload)
	if nc != nil {
		nc.Publish("game.server", data)
		fmt.Println("Result sent:", payload)
	}
}

func ClientPlayCards(nc *nats.Conn, s *Store) {
	// Recebe jogadas dos clientes e usa o Store para resolver a rodada.
	nc.Subscribe("game.client", func(m *nats.Msg) {
		var payload map[string]any
		if err := json.Unmarshal(m.Data, &payload); err != nil {
			log.Println("Error unmarshalling payload:", err)
			return
		}

		gameID := payload["game"].(string)
		clientID := int(payload["client_id"].(float64))
		card := int(payload["card"].(float64))

		// Resolve o duelo entre os jogadores.
		pWin, cardWin, pLose, cardLose, objectId, err := s.PlayCard(nc, gameID, clientID, card)
		if err != nil {
			log.Println("Error executing PlayCard:", err)
			return
		}

		// Gera notificação diferenciada para o vencedor e perdedor.
		response1 := map[string]any{"client_id": pWin.Id, "result": "win", "card": cardLose, "object": objectId}
		response2 := map[string]any{"client_id": pLose.Id, "result": "lose", "card": cardWin, "object": objectId}

		SendingGameResult(response1, nc)
		SendingGameResult(response2, nc)
	})
}

func ClientJoinBlindTrade(nc *nats.Conn, s *Store) {
	// Jogador entra na fila para uma troca às cegas (dois players trocam cartas aleatórias).
	nc.Subscribe("topic.trade.joinBlind", func(m *nats.Msg) {
		var payload map[string]any
		if err := json.Unmarshal(m.Data, &payload); err != nil {
			nc.Publish(m.Reply, []byte(`{"err":"invalid payload"}`))
			return
		}

		clientID := int(payload["client_id"].(float64))
		cardHex := payload["card_id"].(string)

		err := s.JoinBlindTrade(nc, clientID, cardHex)

		if err != nil {
			resp := map[string]any{"err": err.Error()}
			data, _ := json.Marshal(resp)
			nc.Publish(m.Reply, data)
		} else {
			nc.Publish(m.Reply, []byte(`{"status":"queued", "msg":"Você está na fila. Aguarde notificação."}`))
		}
	})
}

func ClientGetCredentials(nc *nats.Conn, s *Store) {
	// Entrega ao cliente os dados da carteira blockchain armazenados no Store.
	nc.Subscribe("topic.getCredentials", func(m *nats.Msg) {
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

		// Envia endereço e chave secreta do jogador para operações on-chain.
		resp := map[string]string{
			"address": player.Wallet.Address,
			"secret":  player.Wallet.Secret,
		}
		data, _ := json.Marshal(resp)
		nc.Publish(m.Reply, data)
	})
}
