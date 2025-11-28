package API

import "sync"

type Store struct {
	mu           sync.Mutex
	players      map[int]Player
	matchHistory map[string]matchStruct
	gameQueue    []int
	Cards        [][3]int
	count        int
	NodeID       string 
}

func NewStore() *Store {
	return &Store{
		players:      make(map[int]Player),
		matchHistory: make(map[string]matchStruct),
		gameQueue:    make([]int, 0),
		count:        0,
		Cards:        setupPacks(900),
		NodeID:       "server-central",
	}
}
