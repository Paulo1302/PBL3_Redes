package API

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

type Wallet struct {
    Address string	`json:"address"`
    Secret  string	`json:"secret"`
}

type IotaRequest struct {
	ClientID 		Wallet 	`json:"client"`
	SecondClientID 	Wallet 	`json:"aux_client"`
	Ok     			bool   	`json:"ok"`
	IotaValue		uint64	`json:"price"`	
}


func RequestCreateWallet(nc *nats.Conn) Wallet {

	response, err := nc.Request("internalServer.wallet", nil, 10 * time.Second)
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
	response, err := nc.Request("internalServer.balance", data, 20 * time.Second)
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
	response, err := nc.Request("internalServer.faucet", data, 20 * time.Second)
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
	response, err := nc.Request("internalServer.transaction", data, 20 * time.Second)
	if err != nil {
		fmt.Println(err.Error())
		return false
	}
	msg := IotaRequest{}
	json.Unmarshal(response.Data, &msg)
	return msg.Ok
}

// --- NOVAS STRUCTS PARA O BLOCKCHAIN ---

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

// --- NOVAS FUNÇÕES DE REQUISIÇÃO ---

// Pede ao TS para criar uma carta (NFT)
func RequestMintCard(nc *nats.Conn, address string, value int) (string, error) {
	req := MintReq{
		Address: address,
		Value:   uint64(value),
	}
	data, _ := json.Marshal(req)
	
	// Timeout maior (10s) pois blockchain demora
	msg, err := nc.Request("internalServer.mintCard", data, 10*time.Second)
	if err != nil {
		return "", err
	}

	var resp map[string]interface{}
	json.Unmarshal(msg.Data, &resp)
	
	if ok, _ := resp["ok"].(bool); ok {
		return resp["digest"].(string), nil
	}
	return "", fmt.Errorf("erro no mint")
}

// Pede ao TS para registrar o resultado
func RequestLogMatch(nc *nats.Conn, winnerAddr, loserAddr string, valWin, valLose int) {
	req := LogMatchReq{
		Winner:  winnerAddr,
		Loser:   loserAddr,
		ValWin:  uint64(valWin),
		ValLose: uint64(valLose),
	}
	data, _ := json.Marshal(req)
	// Fire and forget (não precisamos bloquear esperando resposta crítica)
	nc.Request("internalServer.logMatch", data, 10*time.Second)
}

// Pede ao TS para transferir uma carta (Troca)
func RequestTransferCard(nc *nats.Conn, ownerSecret, objectID, recipientAddr string) error {
	req := TransferReq{
		OwnerSecret:  ownerSecret,
		CardObjectId: objectID,
		Recipient:    recipientAddr,
	}
	data, _ := json.Marshal(req)
	
	msg, err := nc.Request("internalServer.transferCard", data, 10*time.Second)
	if err != nil {
		return err
	}
	
	var resp map[string]interface{}
	json.Unmarshal(msg.Data, &resp)
	if ok, _ := resp["ok"].(bool); !ok {
		return fmt.Errorf("falha na transferência")
	}
	return nil
}