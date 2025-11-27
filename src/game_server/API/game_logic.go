package API

import (
	"fmt"
	"math/rand"
	"sync"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

type IdManager struct{
    Mutex sync.RWMutex
    Count int
    ClientMap map[int]*Player
}

type matchStruct struct {
    SelfId string `json:"self_id"`
    P1     int    `json:"p1"`
    P2     int    `json:"p2"`
    Card1  int    `json:"card1"`
    Card2  int    `json:"card2"`
}

type BattleQueue struct{
    Mutex sync.RWMutex
    ClientQueue []int
}

type TradeQueue struct{
    Mutex sync.RWMutex
    ClientQueue []int
}

type Player struct{
    Id int
	Wallet string
    Cards []int
}

type PackStorage struct{
    Mutex sync.Mutex
    Cards [][3]int
}

/////////////////////////////////////////////////////////
var IM = IdManager{
	Count: 0,
	ClientMap: map[int]*Player{},
}

var BQ = BattleQueue{
    ClientQueue: make([]int, 0),
}

var TQ = TradeQueue{
    ClientQueue: make([]int, 0),
}

var STO = PackStorage{
	Cards : setupPacks(900),
}


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

// --- LÓGICA CENTRALIZADA ---

func (s *Store) CreatePlayer(nc *nats.Conn) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.count += 1
	newPlayer := Player{
		Id:    s.count,
		Wallet: RequestCreateWallet(nc),
		Cards: nil, // ou inicializar vazio
	}
	
	// Lógica que antes estava no Apply:
	s.players[newPlayer.Id] = newPlayer
	fmt.Printf("[Central] Player Created: %d\n", newPlayer.Id)

	return newPlayer.Id, nil
}

func (s *Store) OpenPack(id int) (*[3]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.Cards) == 0 {
		return nil, fmt.Errorf("no packs available")
	}
	
	// Lógica de sorteio
	i := rand.Intn(len(s.Cards))
	lastIndex := len(s.Cards) - 1
	pack := s.Cards[i]
	
	// Remove do deck global
	s.Cards[i] = s.Cards[lastIndex]
	s.Cards = s.Cards[:lastIndex]

	// Adiciona ao player
	player, exists := s.players[id]
	if !exists {
		return nil, fmt.Errorf("player not found")
	}
	player.Cards = append(player.Cards, pack[0], pack[1], pack[2]) 
	s.players[id] = player
	fmt.Println(player.Cards)
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
	
	// Atualiza histórico e remove da fila
	s.matchHistory[gameId] = x
	s.gameQueue = s.gameQueue[2:]
	
	fmt.Println("[Central] Match Created:", gameId, p1, "vs", p2)
	return x, nil
}

func (s *Store) PlayCard(gameId string, id int, card int) error {
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
	return nil
}