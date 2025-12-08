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

// IdManager controla IDs de jogadores e armazena seus dados.
// Usa mutex para permitir acesso concorrente seguro.
type IdManager struct {
	Mutex     sync.RWMutex
	Count     int
	ClientMap map[int]*Player
}

// Estrutura básica que representa uma partida ativa.
// Usada para registrar jogadores, cartas enviadas e o ID único da partida.
type matchStruct struct {
	SelfId string `json:"self_id"`
	P1     int    `json:"p1"`
	P2     int    `json:"p2"`
	Card1  int    `json:"card1"`
	Card2  int    `json:"card2"`
}

// Representa um jogador do servidor: ID, carteira blockchain e suas cartas.
// O mapa Cards armazena "ObjectID da blockchain → poder da carta".
type Player struct {
	Id     int
	Wallet Wallet
	Cards  map[string]int 
}

// Armazena todos os pacotes de cartas que podem ser sorteados ao abrir packs.
type PackStorage struct {
	Mutex sync.Mutex
	Cards [][3]int
}

// Estrutura usada no sistema de troca cega (Blind Trade).
// Guarda quem propôs, quem recebe, as cartas e a data da proposta.
type TradeProposal struct {
	ProposerID   int
	TargetID     int
	ProposerCard string
	TargetCard   string
	CreatedAt    time.Time
}

// --- VARIÁVEIS GLOBAIS ---

// Gerenciador de IDs de jogadores
var IM = IdManager{Count: 0, ClientMap: map[int]*Player{}}

// Armazena todos os packs já pré-gerados
var STO = PackStorage{Cards: setupPacks(900)}

// Endereço da carteira da loja (carregado via .env)
var ServerWalletAddress string

// --- FUNÇÃO INIT ---
func init() {
	// Carrega variáveis do .env usadas pela blockchain
	godotenv.Load("../blockchain_server/.env")
	ServerWalletAddress = os.Getenv("ADDRESS")

	// Mensagem de aviso caso o .env não contenha a carteira
	if ServerWalletAddress == "" {
		log.Println("⚠️ AVISO: Endereço da carteira não encontrado no .env")
	} else {
		fmt.Println("✅ Carteira da Loja Carregada:", ServerWalletAddress)
	}
}

// --- CONFIGURAÇÃO DE PACOTES ---

// Gera uma lista de N cartas numeradas e divide em pacotes de 3.
// Cada pack contém 3 IDs consecutivos, usados ao abrir pacotes.
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

// Cria um novo jogador no sistema, gera uma carteira blockchain
// e armazena tudo na Store.
func (s *Store) CreatePlayer(nc *nats.Conn) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.count += 1
	newPlayer := Player{
		Id:     s.count,
		Wallet: RequestCreateWallet(nc),
		Cards:  make(map[string]int),
	}

	s.players[newPlayer.Id] = newPlayer
	fmt.Println("[Central] Player Created:", newPlayer.Id, newPlayer.Wallet.Address)

	return newPlayer.Id, nil
}

// Abre um pacote de 3 cartas:
// 1) cobra o jogador via blockchain,
// 2) sorteia um pack,
// 3) mint das cartas na blockchain,
// 4) salva as cartas no cache local do jogador.
func (s *Store) OpenPack(nc *nats.Conn, id int) (*[3]int, error) {
	s.mu.Lock()
	player, exists := s.players[id]
	s.mu.Unlock()

	if !exists {
		return nil, fmt.Errorf("player not found")
	}

	// --- ETAPA 1: Cobrança blockchain ---
	serverWallet := Wallet{Address: ServerWalletAddress}

	fmt.Printf("💰 Cobrando 1000 IOTA de %d...\n", id)
	sucesso := RequestTransaction(nc, player.Wallet, serverWallet, 1000)

	if !sucesso {
		return nil, fmt.Errorf("saldo insuficiente ou erro na transação")
	}

	// --- ETAPA 2: Sorteio aleatório de pack ---
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

	// --- ETAPA 3: Mint das cartas ---
	fmt.Println("⚡ Minting cards on blockchain...")
	
	newCards := make(map[string]int)

	for _, cardVal := range pack {
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

	// --- ETAPA 4: Atualiza cache local do jogador ---
	s.mu.Lock()
	p := s.players[id]
	for k, v := range newCards {
		p.Cards[k] = v
	}
	s.players[id] = p
	s.mu.Unlock()

	return &pack, nil
}

// --- LÓGICA DE TROCA CEGRA (BLIND TRADE) ---
// Jogador entra na fila de troca: valida propriedade via blockchain,
// remove a carta do cache, e aguarda Pareamento.
func (s *Store) JoinBlindTrade(nc *nats.Conn, playerID int, cardHex string) error {
	s.mu.Lock()
	player, exists := s.players[playerID]
	s.mu.Unlock()

	if !exists {
		return fmt.Errorf("jogador não encontrado")
	}
	
	// Verifica se a carta está no cache (só aviso; validação real é blockchain)
	if _, hasLocal := player.Cards[cardHex]; !hasLocal {
		fmt.Println("⚠️ Carta não encontrada no cache local (pode ser nova), verificando blockchain...")
	}

	fmt.Printf("🔍 [BlindTrade] Validando propriedade: %s tem %s?\n", player.Wallet.Address, cardHex)
	owns := RequestValidateOwnership(nc, player.Wallet.Address, cardHex)

	if !owns {
		return fmt.Errorf("você não é dono desta carta na blockchain")
	}

	// Cria requisição
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

	// Remove carta para impedir reutilização
	delete(s.players[playerID].Cards, cardHex)

	s.BlindTradeQueue = append(s.BlindTradeQueue, req)
	queueLen := len(s.BlindTradeQueue)
	s.mu.Unlock()

	fmt.Printf("📥 [BlindTrade] Jogador %d entrou na fila. (Total: %d)\n", playerID, queueLen)

	go s.ProcessBlindQueue(nc)

	return nil
}

// Processa pares da fila de forma FIFO, realiza Atomic Swap
// e envia resposta individual para cada jogador via NATS.
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
		} else {
			msgA = fmt.Sprintf(`{"status": "success", "received_card": "%s"}`, userB.CardHex)
			msgB = fmt.Sprintf(`{"status": "success", "received_card": "%s"}`, userA.CardHex)
			fmt.Println("✅ BlindTrade Concluído!")
		}

		nc.Publish(fmt.Sprintf("trade.result.%d", userA.PlayerID), []byte(msgA))
		nc.Publish(fmt.Sprintf("trade.result.%d", userB.PlayerID), []byte(msgB))
	}
}

// --- GAME LOGIC ---

// Coloca jogador na fila de matchmaking
func (s *Store) JoinQueue(id int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gameQueue = append(s.gameQueue, id)
	return id, nil
}

// Cria uma partida quando houver ao menos 2 jogadores na fila.
// Gera UUID como ID da partida e registra no histórico.
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

// Registra a carta jogada pelo jogador e, quando ambas estiverem presentes,
// chama ResolveMatch para decidir o vencedor.
func (s *Store) PlayCard(nc *nats.Conn, gameId string, id int, cardVal int) (Player, int, Player, int, string, error) {
	s.mu.Lock()

	game, exists := s.matchHistory[gameId]
	if !exists { return Player{}, 0, Player{}, 0, "", fmt.Errorf("game not found") }

	// Verifica se jogador realmente tem carta com o valor informado
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

	// Salva a jogada na estrutura da partida
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

	// Se ambos jogaram, resolve partida
	if game.Card1 != 0 && game.Card2 != 0 {
		fmt.Println("Resolving Game")
		return s.ResolveMatch(nc, game)
	}

	return Player{}, 0, Player{}, 0, "", fmt.Errorf("just one player")
}

// Compara valores das cartas, define vencedor, cria log da partida
// na blockchain e retorna resultado para o servidor de jogo.
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
