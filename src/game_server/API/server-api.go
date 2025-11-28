package API

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

type IotaRequest struct {
	ClientID 		string 	`json:"client"`
	SecondClientID 	string 	`json:"aux_client"`
	Ok     			bool   	`json:"ok"`
	IotaValue		uint64	`json:"price"`	
}


func RequestCreateWallet(nc *nats.Conn) string {

	response, err := nc.Request("internalServer.wallet", nil, 10 * time.Second)
	if err != nil {
		fmt.Println(err.Error())
		return ""
	}
	msg := IotaRequest{}
	json.Unmarshal(response.Data, &msg)
	return msg.ClientID
}

func RequestFaucet(nc *nats.Conn, walletId string) uint64 {
	requestData := IotaRequest{ClientID: walletId}
	data, _ := json.Marshal(requestData)
	response, err := nc.Request("internalServer.faucet", data, 10 * time.Second)
	if err != nil {
		fmt.Println(err.Error())
		return 0
	}
	msg := IotaRequest{}
	json.Unmarshal(response.Data, &msg)
	return msg.IotaValue
}