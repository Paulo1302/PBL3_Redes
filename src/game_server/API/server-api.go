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