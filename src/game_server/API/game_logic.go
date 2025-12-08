package API

import (
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
	// CORREÇÃO: Usando mapa para vincular ID da Blockchain (Key) ao Poder da Carta (Value)
	Cards  map[string]int 
}

type PackStorage struct {
	Mutex sync.Mutex
	Cards [][3]int
}

// NOVA ESTRUTURA PARA TROCA P2P
type TradeProposal struct {
	ProposerID   int
	TargetID     int
	ProposerCard string // ID do Objeto na IOTA (Hex String)
	TargetCard   string // ID do Objeto na IOTA (Hex String)
	CreatedAt    time.Time
}

// --- VARIÁVEIS GLOBAIS ---
var IM = IdManager{Count: 0, ClientMap: map[int]*Player{}}
var STO = PackStorage{Cards: setupPacks(900)}
var ServerWalletAddress string

// --- FUNÇÃO INIT ---
func init() {
	godotenv.Load("../blockchain_server/.env")
	ServerWalletAddress = os.Getenv("ADDRESS")
	if ServerWalletAddress == "" {
		log.Println("⚠️ AVISO: Endereço da carteira não encontrado no .env")
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
		Cards:  make(map[string]int), // Inicializa o mapa vazio
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

	fmt.Printf("💰 Cobrando 1000 IOTA de %d...\n", id)
	sucesso := RequestTransaction(nc, player.Wallet, serverWallet, 1000)

	if !sucesso {
		return nil, fmt.Errorf("saldo insuficiente ou erro na transação")
	}

	// 2. SORTEIO
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
	fmt.Println("⚡ Minting cards on blockchain...")
	
	// Mapa temporário para armazenar as novas cartas
	newCards := make(map[string]int)

	for _, cardVal := range pack {
		// CORREÇÃO: Recebe Digest e ObjectID
		digest, objectId, err := RequestMintCard(nc, player.Wallet.Address, cardVal)
		
		if err != nil {
			log.Println("❌ Falha no Mint:", err)
		} else {
			fmt.Printf("✅ Carta %d criada! ID: %s (Digest: %s)\n", cardVal, objectId, digest)
			if objectId != "" {
				newCards[objectId] = cardVal
			}
		}
	}

	// 4. ATUALIZAÇÃO LOCAL
	s.mu.Lock()
	p := s.players[id]
	// Adiciona as novas cartas ao mapa do jogador
	for k, v := range newCards {
		p.Cards[k] = v
	}
	s.players[id] = p
	s.mu.Unlock()

	return &pack, nil
}

// --- LÓGICA DE TROCA SEGURA (BLIND TRADE) ---

func (s *Store) JoinBlindTrade(nc *nats.Conn, playerID int, cardHex string) error {
	s.mu.Lock()
	player, exists := s.players[playerID]
	s.mu.Unlock()

	if !exists {
		return fmt.Errorf("jogador não encontrado")
	}
	
	// 0. Validação Local: Verifica se o jogador possui a carta no cache local
	if _, hasLocal := player.Cards[cardHex]; !hasLocal {
		// Aviso apenas, a validação real é na blockchain
		fmt.Println("⚠️ Carta não encontrada no cache local (pode ser nova), verificando blockchain...")
	}

	// 1. Validação Blockchain
	fmt.Printf("🔍 [BlindTrade] Validando propriedade: %s tem %s?\n", player.Wallet.Address, cardHex)
	owns := RequestValidateOwnership(nc, player.Wallet.Address, cardHex)
	if !owns {
		return fmt.Errorf("você não é dono desta carta na blockchain")
	}

	// 2. Adiciona à fila
	req := BlindTradeRequest{
		PlayerID: playerID,
		CardHex:  cardHex,
		Wallet:   player.Wallet,
	}

	s.mu.Lock()
	for _, r := range s.BlindTradeQueue {
		if r.PlayerID == playerID {
			s.mu.Unlock()
			return fmt.Errorf("você já está na fila de troca")
		}
	}
	
	// Remove a carta do cache local para evitar uso duplo
	delete(s.players[playerID].Cards, cardHex)

	s.BlindTradeQueue = append(s.BlindTradeQueue, req)
	queueLen := len(s.BlindTradeQueue)
	s.mu.Unlock()

	fmt.Printf("📥 [BlindTrade] Jogador %d entrou na fila. (Total: %d)\n", playerID, queueLen)

	go s.ProcessBlindQueue(nc)

	return nil
}

func (s *Store) ProcessBlindQueue(nc *nats.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for len(s.BlindTradeQueue) >= 2 {
		userA := s.BlindTradeQueue[0]
		userB := s.BlindTradeQueue[1]
		s.BlindTradeQueue = s.BlindTradeQueue[2:]

		fmt.Printf("⚡ [BlindTrade] Match! %d <-> %d\n", userA.PlayerID, userB.PlayerID)

		err := RequestAtomicSwap(nc, 
			userA.Wallet, userA.CardHex, 
			userB.Wallet, userB.CardHex, 
		)

		var msgA, msgB string
		
		if err != nil {
			msgA = fmt.Sprintf(`{"status": "error", "msg": "Falha na blockchain: %v"}`, err)
			msgB = msgA
			fmt.Println("❌ Falha no BlindTrade:", err)
			// Idealmente devolveria as cartas ao cache, mas simplificado aqui
		} else {
			msgA = fmt.Sprintf(`{"status": "success", "received_card": "%s"}`, userB.CardHex)
			msgB = fmt.Sprintf(`{"status": "success", "received_card": "%s"}`, userA.CardHex)
			fmt.Println("✅ BlindTrade Concluído!")
			
			// Nota: Atualizar o cache local exigiria saber o VALOR da carta recebida.
			// Como o swap é cego, o cliente terá que dar refresh (Ver Cartas) para atualizar.
		}

		nc.Publish(fmt.Sprintf("trade.result.%d", userA.PlayerID), []byte(msgA))
		nc.Publish(fmt.Sprintf("trade.result.%d", userB.PlayerID), []byte(msgB))
	}
}

// --- GAME LOGIC ---

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

func (s *Store) PlayCard(nc *nats.Conn, gameId string, id int, cardVal int) (Player, int, Player, int, string, error) {
	s.mu.Lock()

	game, exists := s.matchHistory[gameId]
	if !exists { return Player{}, 0, Player{}, 0, "", fmt.Errorf("game not found") }

	// CORREÇÃO: Verifica se o jogador possui uma carta com esse VALOR no mapa
	player := s.players[id]
	hasCard := false
	
	for _, v := range player.Cards {
		if v == cardVal {
			hasCard = true
			break
		}
	}

	if !hasCard {
		s.mu.Unlock()
		return Player{}, 0, Player{}, 0, "", fmt.Errorf("player does not have card with value %d", cardVal)
	}

	if game.P1 == id {
		game.Card1 = cardVal
	} else if game.P2 == id {
		game.Card2 = cardVal
	} else {
		s.mu.Unlock()
		return Player{}, 0, Player{}, 0, "", fmt.Errorf("player not in match")
	}

	s.matchHistory[gameId] = game
	fmt.Println("[Central] Card Played:", cardVal, "in game", gameId)
	
	s.mu.Unlock()

	if game.Card1 != 0 && game.Card2 != 0 {
		fmt.Println("Resolving Game")
		return s.ResolveMatch(nc, game)
	}

	return Player{}, 0, Player{}, 0, "", fmt.Errorf("just one player")
}

func (s *Store) ResolveMatch(nc *nats.Conn, game matchStruct) (Player, int, Player, int, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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
		return Player{}, 0, Player{}, 0, "", fmt.Errorf("unexpected draw")
	}

	pWin := s.players[winnerID]
	pLose := s.players[loserID]

	fmt.Printf("🏆 Vencedor: Player %d (Carta %d)\n", winnerID, winVal)

	digest, objectId, err := RequestLogMatch(nc, pWin.Wallet.Address, pLose.Wallet.Address, winVal, loseVal)
	if err != nil {
		log.Println("❌ Falha no log:", err)
	} else {
		fmt.Printf("✅ Log criado! ID: %s (Digest: %s)\n", objectId, digest)
		if objectId != "" {
			return pWin, winVal, pLose, loseVal, objectId, nil
		}
	}
	return Player{}, 0, Player{}, 0, "", nil
}