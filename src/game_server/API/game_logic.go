package API

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/nats-io/nats.go"
)

// --- ESTRUTURAS ---
type IdManager struct {
	Mutex     sync.RWMutex
	Count     int
	ClientMap map[int]*Player
}

type matchStruct struct {
	SelfId string `json:"self_id"`
	P1     int    `json:"p1"`
	P2     int    `json:"p2"`
	Card1  int    `json:"card1"`
	Card2  int    `json:"card2"`
}

type Player struct {
	Id     int
	Wallet Wallet
	Cards  []int
}

type PackStorage struct {
	Mutex sync.Mutex
	Cards [][3]int
}

// --- REMOVIDO: MintReq (Já está definido em server-api.go) ---

// --- VARIÁVEIS GLOBAIS ---
var IM = IdManager{Count: 0, ClientMap: map[int]*Player{}}
var STO = PackStorage{Cards: setupPacks(900)}

// Variável para guardar o endereço do servidor
var ServerWalletAddress string

// --- FUNÇÃO INIT (Roda antes de tudo) ---
func init() {
	// 1. Tenta carregar .env da pasta atual (game_server/.env)
	godotenv.Load()

	// 2. Se a variável ainda estiver vazia, tenta carregar da pasta vizinha (blockchain_server/.env)
	if os.Getenv("SERVER_WALLET_ADDRESS") == "" {
		// "../blockchain_server/.env" sobe um nível e entra na outra pasta
		err := godotenv.Load("../blockchain_server/.env")
		if err != nil {
			log.Println("⚠️ Aviso: Não consegui ler '../blockchain_server/.env'")
		}
	}

	// 3. Busca o endereço final
	ServerWalletAddress = os.Getenv("SERVER_WALLET_ADDRESS") 

    if ServerWalletAddress == "" {

        ServerWalletAddress = os.Getenv("ADDRESS") 
    }

	if ServerWalletAddress == "" {
		log.Fatal("❌ ERRO CRÍTICO: Não encontrei o endereço da carteira em nenhum .env!")
	} else {
		fmt.Println("✅ Carteira da Loja Carregada:", ServerWalletAddress)
	}
}

// --- CONFIGURAÇÃO DE PACOTES ---
func setupPacks(N int) [][3]int {
	arr := make([]int, N)
	for i := range N {
		arr[i] = i + 1
	}
	numPacks := (N + 3 - 1) / 3
	packs := make([][3]int, numPacks)
	for i := range numPacks {
		start := i * 3
		end := start + 3
		sliceEnd := min(end, N)
		sourceSlice := arr[start:sliceEnd]
		copy(packs[i][:], sourceSlice)
	}
	return packs
}

// --- STORE METHODS ---

func (s *Store) CreatePlayer(nc *nats.Conn) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.count += 1
	newPlayer := Player{
		Id:     s.count,
		Wallet: RequestCreateWallet(nc),
		Cards:  nil,
	}

	s.players[newPlayer.Id] = newPlayer
	fmt.Println("[Central] Player Created:", newPlayer.Id, newPlayer.Wallet.Address)

	return newPlayer.Id, nil
}

func (s *Store) OpenPack(nc *nats.Conn, id int) (*[3]int, error) {
	s.mu.Lock()
	player, exists := s.players[id]
	s.mu.Unlock()

	if !exists {
		return nil, fmt.Errorf("player not found")
	}

	// 1. COBRANÇA (IOTA)
	serverWallet := Wallet{Address: ServerWalletAddress}

	fmt.Printf("💰 Cobrando 1000 IOTA de %d para a Loja (%s)...\n", id, ServerWalletAddress)
	sucesso := RequestTransaction(nc, player.Wallet, serverWallet, 1000)

	if !sucesso {
		return nil, fmt.Errorf("saldo insuficiente ou erro na transação")
	}

	// 2. SORTEIO E ATUALIZAÇÃO DO ESTOQUE
	s.mu.Lock()
	if len(s.Cards) == 0 {
		s.mu.Unlock()
		return nil, fmt.Errorf("no packs available")
	}

	i := rand.Intn(len(s.Cards))
	lastIndex := len(s.Cards) - 1
	pack := s.Cards[i]

	s.Cards[i] = s.Cards[lastIndex]
	s.Cards = s.Cards[:lastIndex]
	s.mu.Unlock()

	// 3. MINT NA BLOCKCHAIN
	fmt.Println("⚡ Enviando pedido de Mint para a Blockchain...")

	for _, cardVal := range pack {
		req := MintReq{
			Address: player.Wallet.Address,
			// --- CORREÇÃO AQUI ---
			Value:   uint64(cardVal), // Converte int para uint64 explicitamente
			// ---------------------
		}
		reqData, _ := json.Marshal(req)

		msg, err := nc.Request("internalServer.mintCard", reqData, 10*time.Second)

		if err != nil {
			log.Println("❌ Falha no Mint:", err)
		} else {
			var resp map[string]interface{}
			json.Unmarshal(msg.Data, &resp)
			if ok, _ := resp["ok"].(bool); ok {
				fmt.Printf("✅ Carta %d criada! Digest: %s\n", cardVal, resp["digest"])
			}
		}
	}

	// 4. ATUALIZAÇÃO LOCAL
	s.mu.Lock()
	p := s.players[id]
	p.Cards = append(p.Cards, pack[0], pack[1], pack[2])
	s.players[id] = p
	s.mu.Unlock()

	fmt.Println("[Central] Pack opened for player", id, pack)
	return &pack, nil
}

func (s *Store) JoinQueue(id int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.gameQueue = append(s.gameQueue, id)

	return id, nil
}

func (s *Store) CreateMatch() (matchStruct, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.gameQueue) < 2 {
		return matchStruct{}, fmt.Errorf("not enough players")
	}

	p1 := s.gameQueue[0]
	p2 := s.gameQueue[1]
	gameId := uuid.New().String()

	x := matchStruct{
		P1: p1, P2: p2, SelfId: gameId, Card1: 0, Card2: 0,
	}

	s.matchHistory[gameId] = x
	s.gameQueue = s.gameQueue[2:]

	fmt.Println("[Central] Match Created:", gameId, p1, "vs", p2)
	return x, nil
}

func (s *Store) PlayCard(nc *nats.Conn, gameId string, id int, card int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	game, exists := s.matchHistory[gameId]
	if !exists {
		return fmt.Errorf("game not found")
	}

	if game.P1 == id {
		game.Card1 = card
	} else if game.P2 == id {
		game.Card2 = card
	} else {
		return fmt.Errorf("player not in match")
	}

	s.matchHistory[gameId] = game
	fmt.Println("[Central] Card Played:", card, "in game", gameId)

	if game.Card1 != 0 && game.Card2 != 0 {
		// Executa a resolução do jogo
		go s.ResolveMatch(nc, gameId)
	}

	return nil
}

func (s *Store) ResolveMatch(nc *nats.Conn, gameId string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	game, exists := s.matchHistory[gameId]
	if !exists {
		return
	}

	var winnerID, loserID int
	var winVal, loseVal int

	if game.Card1 > game.Card2 {
		winnerID, loserID = game.P1, game.P2
		winVal, loseVal = game.Card1, game.Card2
	} else if game.Card2 > game.Card1 {
		winnerID, loserID = game.P2, game.P1
		winVal, loseVal = game.Card2, game.Card1
	} else {
		fmt.Println("Empate! Ninguém ganha.")
		return
	}

	pWin := s.players[winnerID]
	pLose := s.players[loserID]

	fmt.Printf("🏆 Vencedor: Player %d (Carta %d)\n", winnerID, winVal)

	// Registra na blockchain
	go func() {
		RequestLogMatch(nc, pWin.Wallet.Address, pLose.Wallet.Address, winVal, loseVal)
		fmt.Println("[Blockchain] Log de partida enviado para processamento.")
	}()
}