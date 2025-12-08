package API

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

type Wallet struct {
	Address string `json:"address"`
	Secret  string `json:"secret"`
}

type IotaRequest struct {
	ClientID       Wallet `json:"client"`
	SecondClientID Wallet `json:"aux_client"`
	Ok             bool   `json:"ok"`
	IotaValue      uint64 `json:"price"`
}

type ValidateReq struct {
	Address  string `json:"address"`
	ObjectId string `json:"objectId"`
}

type AtomicSwapReq struct {
	UserA_Addr   string `json:"userA_Addr"`
	UserA_Secret string `json:"userA_Secret"`
	CardA_ID     string `json:"cardA_ID"`

	UserB_Addr   string `json:"userB_Addr"`
	UserB_Secret string `json:"userB_Secret"`
	CardB_ID     string `json:"cardB_ID"`
}

// --- STRUCTS PARA O BLOCKCHAIN SERVER ---

type MintReq struct {
	Address string `json:"address"`
	Value   uint64 `json:"value"`
}

type LogMatchReq struct {
	Winner  string `json:"winner"`
	Loser   string `json:"loser"`
	ValWin  uint64 `json:"val_win"`
	ValLose uint64 `json:"val_lose"`
}

type TransferReq struct {
	OwnerSecret  string `json:"ownerSecret"`  // Segredo de quem envia
	CardObjectId string `json:"cardObjectId"` // ID do NFT na IOTA
	Recipient    string `json:"recipient"`    // Endereço de quem recebe
}

type CardDTO struct {
	ID    string `json:"id"`
	Power int    `json:"power"`
}

type GetCardsResponse struct {
	Ok    bool      `json:"ok"`
	Cards []CardDTO `json:"cards"`
	Error string    `json:"error"`
}

// --- FUNÇÕES DE CARTEIRA E ECONOMIA ---

func RequestCreateWallet(nc *nats.Conn) Wallet {
	response, err := nc.Request("internalServer.wallet", nil, 10*time.Second)
	if err != nil {
		fmt.Println(err.Error())
		return Wallet{}
	}
	msg := IotaRequest{}
	json.Unmarshal(response.Data, &msg)
	return msg.ClientID
}

func RequestBalance(nc *nats.Conn, wallet Wallet) uint64 {
	requestData := IotaRequest{ClientID: wallet}
	data, _ := json.Marshal(requestData)
	response, err := nc.Request("internalServer.balance", data, 20*time.Second)
	if err != nil {
		fmt.Println(err.Error())
		return 0
	}
	msg := IotaRequest{}
	json.Unmarshal(response.Data, &msg)
	return msg.IotaValue
}

func RequestFaucet(nc *nats.Conn, wallet Wallet) uint64 {
	requestData := IotaRequest{ClientID: wallet}
	data, _ := json.Marshal(requestData)
	response, err := nc.Request("internalServer.faucet", data, 20*time.Second)
	if err != nil {
		fmt.Println(err.Error())
		return 0
	}
	msg := IotaRequest{}
	json.Unmarshal(response.Data, &msg)
	return msg.IotaValue
}

func RequestTransaction(nc *nats.Conn, source Wallet, destination Wallet, value int) bool {
	requestData := IotaRequest{ClientID: source, SecondClientID: destination, IotaValue: uint64(value)}
	data, _ := json.Marshal(requestData)
	response, err := nc.Request("internalServer.transaction", data, 20*time.Second)
	if err != nil {
		fmt.Println(err.Error())
		return false
	}
	msg := IotaRequest{}
	json.Unmarshal(response.Data, &msg)
	return msg.Ok
}

// --- FUNÇÕES DE JOGO (NFTs) ---

// RequestMintCard pede ao TS para criar uma carta (NFT).
// Retorna: (Digest da Transação, ObjectID da Carta, Erro)
func RequestMintCard(nc *nats.Conn, address string, value int) (string, string, error) {
	req := MintReq{
		Address: address,
		Value:   uint64(value),
	}
	data, _ := json.Marshal(req)

	// Timeout maior (10s) pois blockchain demora
	msg, err := nc.Request("internalServer.mintCard", data, 10*time.Second)
	if err != nil {
		return "", "", err
	}

	var resp map[string]any
	json.Unmarshal(msg.Data, &resp)

	if ok, _ := resp["ok"].(bool); ok {
		digest := resp["digest"].(string)
		// Recupera o ID do objeto retornado pelo TypeScript para vincular ao Player.Cards
		objectId, _ := resp["objectId"].(string)
		return digest, objectId, nil
	}
	return "", "", fmt.Errorf("erro no mint: %v", resp["error"])
}

// RequestLogMatch pede ao TS para registrar o resultado da partida on-chain
func RequestLogMatch(nc *nats.Conn, winnerAddr, loserAddr string, valWin, valLose int) (string, string, error){
	req := LogMatchReq{
		Winner:  winnerAddr,
		Loser:   loserAddr,
		ValWin:  uint64(valWin),
		ValLose: uint64(valLose),
	}
	data, _ := json.Marshal(req)
	// Request com timeout para garantir que o servidor recebeu, mesmo que não esperemos a confirmação final
	msg,err := nc.Request("internalServer.logMatch", data, 10*time.Second)
	if err != nil {
		return "", "", err
	}

	var resp map[string]any
	json.Unmarshal(msg.Data, &resp)

	if ok, _ := resp["ok"].(bool); ok {
		digest := resp["digest"].(string)
		// Recupera o ID do objeto retornado pelo TypeScript para vincular ao Player.Cards
		objectId, _ := resp["objectId"].(string)
		return digest, objectId, nil
	}
	return "", "", fmt.Errorf("erro no mint: %v", resp["error"])
}

// --- FUNÇÕES DE TRANSFERÊNCIA E TROCA ---

// RequestTransferCard realiza uma transferência simples de uma carta (Unidirecional)
// Útil para presentes ou movimentações administrativas.
func RequestTransferCard(nc *nats.Conn, ownerSecret, cardObjectID, recipientAddr string) error {
	req := TransferReq{
		OwnerSecret:  ownerSecret,
		CardObjectId: cardObjectID,
		Recipient:    recipientAddr,
	}
	data, _ := json.Marshal(req)

	msg, err := nc.Request("internalServer.transferCard", data, 10*time.Second)
	if err != nil {
		return err
	}

	var resp map[string]any
	json.Unmarshal(msg.Data, &resp)

	if ok, _ := resp["ok"].(bool); !ok {
		return fmt.Errorf("falha na transferência")
	}
	return nil
}

// RequestValidateOwnership valida se uma carta pertence a um usuário na Blockchain
func RequestValidateOwnership(nc *nats.Conn, address, objectId string) bool {
	req := ValidateReq{Address: address, ObjectId: objectId}
	data, _ := json.Marshal(req)

	msg, err := nc.Request("internalServer.validateOwnership", data, 5*time.Second)
	if err != nil {
		fmt.Println("Erro validando ownership:", err)
		return false
	}

	var resp map[string]any
	json.Unmarshal(msg.Data, &resp)

	if ok, exists := resp["ok"].(bool); exists {
		return ok
	}
	return false
}

// RequestAtomicSwap executa a troca atômica (Bidirecional e Segura)
func RequestAtomicSwap(nc *nats.Conn, userA Wallet, cardA string, userB Wallet, cardB string) error {
	req := AtomicSwapReq{
		UserA_Addr:   userA.Address,
		UserA_Secret: userA.Secret,
		CardA_ID:     cardA,
		UserB_Addr:   userB.Address,
		UserB_Secret: userB.Secret,
		CardB_ID:     cardB,
	}
	data, _ := json.Marshal(req)

	// Timeout longo para garantir processamento do bloco (Multi-Agent Transaction é pesada)
	msg, err := nc.Request("internalServer.atomicSwap", data, 20*time.Second)
	if err != nil {
		return err
	}

	var resp map[string]any
	json.Unmarshal(msg.Data, &resp)

	if ok, _ := resp["ok"].(bool); !ok {
		return fmt.Errorf("falha na troca: %v", resp["error"])
	}
	return nil
}
func RequestGetCardsFromChain(nc *nats.Conn, address string) ([]CardDTO, error) {
	req := map[string]string{"address": address}
	data, _ := json.Marshal(req)

	// Timeout de 5s é suficiente para leitura
	msg, err := nc.Request("internalServer.getCards", data, 5*time.Second)
	if err != nil {
		return nil, err
	}

	var resp GetCardsResponse
	json.Unmarshal(msg.Data, &resp)

	if !resp.Ok {
		return nil, fmt.Errorf("erro blockchain: %s", resp.Error)
	}

	return resp.Cards, nil
}