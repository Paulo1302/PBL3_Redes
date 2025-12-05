package API

import "sync"

type Store struct {
	mu           sync.Mutex
	players      map[int]Player
	matchHistory map[string]matchStruct
	tradeHistory map[string]matchStruct
	gameQueue    []int
	tradeQueue   []Trade
	Cards        [][3]int
	count        int
	NodeID       string 
}

func NewStore() *Store {
	return &Store{
		players:      make(map[int]Player),
		matchHistory: make(map[string]matchStruct),
		tradeHistory: make(map[string]matchStruct),
		gameQueue:    make([]int, 0),
		tradeQueue:   make([]Trade, 0),
		count:        0,
		Cards:        setupPacks(900),
		NodeID:       "server-central",
	}
}
