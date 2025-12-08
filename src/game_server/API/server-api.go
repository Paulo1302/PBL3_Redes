package API

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

//
// ------------------------------
//         TIPOS DO SISTEMA
// ------------------------------
//

// Wallet representa uma carteira da blockchain (endereço + segredo)
type Wallet struct {
	Address string `json:"address"`
	Secret  string `json:"secret"`
}

// Estrutura padrão usada em diversas requisições relacionadas ao token IOTA
type IotaRequest struct {
	ClientID       Wallet `json:"client"`       // Carteira do usuário A
	SecondClientID Wallet `json:"aux_client"`   // Carteira do usuário B (quando necessário)
	Ok             bool   `json:"ok"`           // Resultado da operação
	IotaValue      uint64 `json:"price"`        // Valor movimentado
}

// Estrutura usada para validar se uma carta pertence a um usuário
type ValidateReq struct {
	Address  string `json:"address"`
	ObjectId string `json:"objectId"`
}

// Estrutura usada na troca atômica (atomic swap)
type AtomicSwapReq struct {
	UserA_Addr   string `json:"userA_Addr"`
	UserA_Secret string `json:"userA_Secret"`
	CardA_ID     string `json:"cardA_ID"`

	UserB_Addr   string `json:"userB_Addr"`
	UserB_Secret string `json:"userB_Secret"`
	CardB_ID     string `json:"cardB_ID"`
}

//
// ------ STRUCTS PARA COMUNICAÇÃO COM O SERVIDOR BLOCKCHAIN ------
//

// Requisição para criar um novo NFT
type MintReq struct {
	Address string `json:"address"` // Dono inicial da carta
	Value   uint64 `json:"value"`   // Valor em tokens da carta
}

// Registrar o resultado de um duelo da partida
type LogMatchReq struct {
	Winner  string `json:"winner"`
	Loser   string `json:"loser"`
	ValWin  uint64 `json:"val_win"`
	ValLose uint64 `json:"val_lose"`
}

// Estrutura usada para transferência de cartas (simples)
type TransferReq struct {
	OwnerSecret  string `json:"ownerSecret"`  // Chave secreta do remetente
	CardObjectId string `json:"cardObjectId"` // ID do NFT
	Recipient    string `json:"recipient"`    // Endereço de destino
}

// DTO representando cartas retornadas pela blockchain
type CardDTO struct {
	ID    string `json:"id"`
	Power int    `json:"power"`
}

// Resposta padrão da função GetCards
type GetCardsResponse struct {
	Ok    bool      `json:"ok"`
	Cards []CardDTO `json:"cards"`
	Error string    `json:"error"`
}

//
// ------------------------------
//   FUNÇÕES DE CARTEIRA E ECONOMIA
// ------------------------------
//

// Solicita ao servidor a criação de uma nova carteira blockchain
func RequestCreateWallet(nc *nats.Conn) Wallet {
	// Envia request ao servidor interno sem payload
	response, err := nc.Request("internalServer.wallet", nil, 10*time.Second)
	if err != nil {
		fmt.Println(err.Error())
		return Wallet{}
	}

	// Decodifica a resposta
	msg := IotaRequest{}
	json.Unmarshal(response.Data, &msg)
	return msg.ClientID
}

// Obtém saldo de uma carteira
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

// Solicita tokens no faucet (teste)
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

// Realiza uma transação entre dois usuários
func RequestTransaction(nc *nats.Conn, source Wallet, destination Wallet, value int) bool {
	// Monta payload da transação
	requestData := IotaRequest{
		ClientID:       source,
		SecondClientID: destination,
		IotaValue:      uint64(value),
	}

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

//
// ------------------------------
//         FUNÇÕES DE NFTs
// ------------------------------
//

// Solicita a criação de uma carta NFT no blockchain
func RequestMintCard(nc *nats.Conn, address string, value int) (string, string, error) {
	req := MintReq{Address: address, Value: uint64(value)}
	data, _ := json.Marshal(req)

	// Mint é lento → timeout maior
	msg, err := nc.Request("internalServer.mintCard", data, 10*time.Second)
	if err != nil {
		return "", "", err
	}

	var resp map[string]any
	json.Unmarshal(msg.Data, &resp)

	// Verifica sucesso
	if ok, _ := resp["ok"].(bool); ok {
		digest := resp["digest"].(string)
		objectId := resp["objectId"].(string)
		return digest, objectId, nil
	}

	return "", "", fmt.Errorf("erro no mint: %v", resp["error"])
}

// Registra uma partida na blockchain
func RequestLogMatch(nc *nats.Conn, winnerAddr, loserAddr string, valWin, valLose int) (string, string, error) {
	req := LogMatchReq{
		Winner:  winnerAddr,
		Loser:   loserAddr,
		ValWin:  uint64(valWin),
		ValLose: uint64(valLose),
	}

	data, _ := json.Marshal(req)

	msg, err := nc.Request("internalServer.logMatch", data, 10*time.Second)
	if err != nil {
		return "", "", err
	}

	var resp map[string]any
	json.Unmarshal(msg.Data, &resp)

	if ok, _ := resp["ok"].(bool); ok {
		return resp["digest"].(string), resp["objectId"].(string), nil
	}

	return "", "", fmt.Errorf("erro no mint: %v", resp["error"])
}

//
// ------------------------------
//   FUNÇÕES DE TRANSFERÊNCIA E TROCA
// ------------------------------
//

// Transferência simples de NFT (não atômica, unidirecional)
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

// Valida se um NFT pertence a um usuário
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

	// Se existir "ok", retorna
	if ok, exists := resp["ok"].(bool); exists {
		return ok
	}
	return false
}

// Execução de uma troca atômica entre dois usuários
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

	// Atomic swap envolve múltiplas transações → timeout maior
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

// Obtém todas as cartas pertencentes a um usuário na blockchain
func RequestGetCardsFromChain(nc *nats.Conn, address string) ([]CardDTO, error) {
	req := map[string]string{"address": address}
	data, _ := json.Marshal(req)

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
