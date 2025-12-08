package API

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
)

// --- ESTRUTURAS EXPORTADAS ---

// CardDisplay representa uma carta vista pelo cliente no menu.
// Contém ID (string) e poder numérico.
type CardDisplay struct {
	ID    string `json:"id"`
	Power int    `json:"power"`
}

// Estrutura para mostrar ao usuário suas credenciais armazenadas na blockchain.
// Usada na opção “Ver credenciais”.
type UserCredentials struct {
	Address string `json:"address"`
	Secret  string `json:"secret"`
}

// Estruturas internas usadas apenas para comunicação interna do matchmaking.

type matchStruct struct {
	SelfId string `json:"self_id"`
	P1     int    `json:"p1"`
	P2     int    `json:"p2"`
	Card1  int    `json:"card1"`
	Card2  int    `json:"card2"`
}

type NatsMessage struct {
	ClientID int         `json:"client_id"`
	Err      any         `json:"err"`
	Match    matchStruct `json:"match"`
}

// --- INFRAESTRUTURA ---

// BrokerConnect conecta ao servidor NATS baseado no número fornecido.
// Cada servidor NATS está em localhost com offset +4222.
func BrokerConnect(serverNumber int) *nats.Conn {
	url := "nats://localhost:" + strconv.Itoa(serverNumber+4222)
	nc, _ := nats.Connect(url)
	return nc
}

// RequestPing mede o ping entre cliente e servidor através de um request NATS.
// Retorna latência em ms ou -1 se ocorreu erro.
func RequestPing(nc *nats.Conn) int64 {
	msg := map[string]any{
		"send_time": time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(msg)
	response, err := nc.Request("topic.ping", data, 5*time.Second)
	if err != nil {
		return -1
	}
	json.Unmarshal(response.Data, &msg)
	if msg["server_ping"] == nil {
		return -1
	}
	return int64(msg["server_ping"].(float64)) - int64(msg["send_time"].(float64))
}

// --- CONTA E LOGIN ---

// RequestCreateAccount cria uma conta blockchain no servidor.
// Retorna o ID do jogador criado ou 0 caso erro.
func RequestCreateAccount(nc *nats.Conn) int {
	response, err := nc.Request("topic.createAccount", nil, 10*time.Second)
	if err != nil {
		fmt.Println("Erro NATS:", err.Error())
		return 0
	}
	msg := make(map[string]int)
	json.Unmarshal(response.Data, &msg)
	return msg["player_id"]
}

// RequestLogin tenta realizar login usando o ID do jogador.
// Retorna sucesso (bool) e um erro caso a autenticação falhe.
func RequestLogin(nc *nats.Conn, id int) (bool, error) {
	msg := map[string]any{
		"client_id": id,
	}
	data, _ := json.Marshal(msg)
	response, err := nc.Request("topic.login", data, 10*time.Second)
	if err != nil {
		return false, err
	}
	json.Unmarshal(response.Data, &msg)

	if msg["err"] != nil {
		return false, errors.New(msg["err"].(string))
	}
	return msg["result"].(bool), nil
}

// --- ECONOMIA (PACOTES E CARTAS) ---

// RequestOpenPack solicita ao servidor a abertura de um pacote.
// Retorna slice de IDs das cartas recebidas.
func RequestOpenPack(nc *nats.Conn, id int) ([]int, error) {
	msg := map[string]any{
		"client_id": id,
	}
	data, _ := json.Marshal(msg)
	response, err := nc.Request("topic.openPack", data, 60*time.Second)

	if err != nil {
		return nil, err
	}

	json.Unmarshal(response.Data, &msg)

	if msg["err"] != nil {
		return nil, errors.New(msg["err"].(string))
	}

	if msg["result"] == nil {
		return []int{}, nil
	}

	resultSlice := msg["result"].([]any)
	cards := make([]int, 0, len(resultSlice))

	for _, item := range resultSlice {
		cards = append(cards, int(item.(float64)))
	}

	return cards, nil
}

// RequestSeeCards retorna todas as cartas que o usuário possui,
// já no formato CardDisplay.
func RequestSeeCards(nc *nats.Conn, id int) ([]CardDisplay, error) {
	msg := map[string]any{
		"client_id": id,
	}
	data, _ := json.Marshal(msg)
	response, err := nc.Request("topic.seeCards", data, 10*time.Second)

	if err != nil {
		return nil, err
	}

	var rawResp struct {
		Result []CardDisplay `json:"result"`
		Err    string        `json:"err"`
	}

	if err := json.Unmarshal(response.Data, &rawResp); err != nil {
		return nil, fmt.Errorf("erro parse json: %v", err)
	}

	if rawResp.Err != "" {
		return nil, errors.New(rawResp.Err)
	}

	return rawResp.Result, nil
}

// --- MATCHMAKING ---

// RequestFindMatch envia pedido para entrar na fila de partida e aguarda pareamento.
// O servidor responde por broadcast no tópico matchmaking.
func RequestFindMatch(nc *nats.Conn, id int) (string, error) {
	matchValue := ""
	match := &matchValue
	onQueue := make(chan int)

	// Inscrição temporária no canal de matchmaking para capturar resposta destinada ao jogador.
	sub, _ := nc.Subscribe("topic.matchmaking", func(msg *nats.Msg) {
		var natsPayload NatsMessage
		json.Unmarshal(msg.Data, &natsPayload)
		if natsPayload.ClientID != id {
			return
		}
		// Em caso de erro do servidor
		if natsPayload.Err != nil {
			*match = ""
			onQueue <- -1
			return
		}

		// Confirma ao servidor o recebimento da mensagem
		nc.Publish(msg.Reply, msg.Data)
		*match = natsPayload.Match.SelfId
		onQueue <- 0
	})
	
	defer sub.Unsubscribe()

	msg := map[string]any{
		"client_id": id,
	}
	data, _ := json.Marshal(msg)
	response, err := nc.Request("topic.findMatch", data, 30*time.Second)

	if err != nil {
		return "", err
	}

	json.Unmarshal(response.Data, &msg)
	if msg["err"] != nil {
		return "", errors.New(msg["err"].(string))
	}

	// Aguarda resposta do servidor ou timeout
	select {
	case res := <-onQueue:
		if res == -1 {
			return "", errors.New("erro na fila")
		}
		return *match, nil
	case <-time.After(60 * time.Second):
		return "", errors.New("timeout matchmaking")
	}
}

// --- BLIND TRADE ---

// JoinBlindTrade envia uma carta para participar de uma troca cega.
func JoinBlindTrade(nc *nats.Conn, myID int, myCard string) error {
	req := map[string]any{
		"client_id": myID,
		"card_id":   myCard,
	}
	data, _ := json.Marshal(req)
	_, err := nc.Request("topic.trade.joinBlind", data, 5*time.Second)
	return err
}

// WaitForTradeResult aguarda pelo resultado da troca cega.
// Notificação enviada no canal dedicado ao jogador.
func WaitForTradeResult(nc *nats.Conn, myID int) {
	ch := make(chan struct{})
	
	sub, _ := nc.Subscribe(fmt.Sprintf("trade.result.%d", myID), func(m *nats.Msg) {
		var resp map[string]any
		json.Unmarshal(m.Data, &resp)

		fmt.Println("\n\n🔔 NOTIFICAÇÃO DE TROCA RECEBIDA!")
		if resp["status"] == "success" {
			fmt.Println("===============================================")
			fmt.Println("🎉 TROCA REALIZADA COM SUCESSO!")
			fmt.Printf("🃏 Você enviou sua carta e RECEBEU ID: %s\n", resp["received_card"])
			fmt.Printf("🔗 Prova: Transação Atômica Confirmada\n")
			fmt.Println("===============================================")
		} else {
			fmt.Println("❌ A troca falhou:", resp["msg"])
		}
		ch <- struct{}{}
	})
	
	<-ch
	sub.Unsubscribe()
}

// --- CREDENCIAIS ---

// RequestCredentials pede ao servidor o par (address, secret)
// para exibir ao usuário. O servidor retorna erro se não existirem.
func RequestCredentials(nc *nats.Conn, id int) (*UserCredentials, error) {
	msg := map[string]any{
		"client_id": id,
	}
	data, _ := json.Marshal(msg)
	
	response, err := nc.Request("topic.getCredentials", data, 5*time.Second)
	if err != nil {
		return nil, err
	}

	var raw map[string]string
	if err := json.Unmarshal(response.Data, &raw); err != nil {
		return nil, err
	}
	
	if val, ok := raw["err"]; ok {
		return nil, errors.New(val)
	}

	return &UserCredentials{
		Address: raw["address"],
		Secret:  raw["secret"],
	}, nil
}

// --- GAME LOOP ---

// SendCards envia ao servidor a carta jogada e o identificador da partida.
func SendCards(nc *nats.Conn, id int, card int, game string) {
	msg := map[string]any{
		"client_id": id,
		"card":      card,
		"game":      game,
	}
	data, _ := json.Marshal(msg)
	nc.Publish("game.client", data)
}

// ManageGame2 escuta mensagens de jogo enviadas pelo servidor
// e distribui para os canais card, roundResult e object.
func ManageGame2(nc *nats.Conn, id *int, card chan int, roundResult chan string, object chan string) {
	nc.Subscribe("game.server", func(msg *nats.Msg) {
		var payload map[string]any
		json.Unmarshal(msg.Data, &payload)
		
		currId := *id
		if currId == 0 { return }
		
		// Se o servidor sinalizou erro
		if payload["err"] != nil {
			card <- 0
			roundResult <- "error"
			return
		}
		
		pID, _ := payload["client_id"].(float64)
		if int(pID) != currId { return }

		card <- int(payload["card"].(float64))
		roundResult <- payload["result"].(string)
		object <- payload["object"].(string)
	})
}

// ImAlive responde ao servidor com heartbeat enquanto estiver conectado.
func ImAlive(nc *nats.Conn, id int) *nats.Subscription {
	sub, _ := nc.Subscribe("game.heartbeat", func(m *nats.Msg) {
		var payload map[string]int
		json.Unmarshal(m.Data, &payload)
		if int(payload["client_id"]) != id {
			return
		}
		nc.Publish(m.Reply, m.Data)
	})
	return sub
}

// LoggedIn confirma ao servidor que o jogador segue conectado
// quando solicitado pelo tópico loggedIn.
func LoggedIn(nc *nats.Conn, id int) *nats.Subscription {
	sub, _ := nc.Subscribe("topic.loggedIn", func(m *nats.Msg) {
		var payload map[string]int
		json.Unmarshal(m.Data, &payload)
		if int(payload["client_id"]) != id {
			return
		}
		nc.Publish(m.Reply, m.Data)
	})
	return sub
}

// Heartbeat registra e atualiza o ping vindo do servidor
// e aumenta os limites do buffer para evitar slow consumer.
func Heartbeat(nc *nats.Conn, value *int64) {
	sub, err := nc.Subscribe("topic.heartbeat", func(msg *nats.Msg) {
		var ping map[string]int64
		if err := json.Unmarshal(msg.Data, &ping); err == nil {
			*value = ping["server_ping"]
		}
	})
	
	if err == nil {
		// Ajusta limites grandes para evitar travamento ao receber muitos heartbeats.
		sub.SetPendingLimits(65536, 64*1024*1024)
	}
}
