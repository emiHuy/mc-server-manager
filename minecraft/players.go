package minecraft

import (
	"slices"
	"sync"
)

type playerSet struct {
	players map[string]struct{}
	mu      sync.Mutex // guards players
}

func newPlayerSet() *playerSet {
	return &playerSet{players: make(map[string]struct{})}
}

func (ps *playerSet) add(player string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.players[player] = struct{}{}
}

func (ps *playerSet) remove(player string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	delete(ps.players, player)
}

func (ps *playerSet) clear() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	clear(ps.players)
}

func (ps *playerSet) list() []string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	playerList := make([]string, 0, len(ps.players))
	for player := range ps.players {
		playerList = append(playerList, player)
	}

	slices.Sort(playerList)
	return playerList
}
