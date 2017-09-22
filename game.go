package main

import (
	"github.com/gorilla/websocket"
	"strings"
	"time"

	"fmt"
)

type class int

const (
	CitizenClass class = iota
	MafiaClass
	DetectiveClass
	DoctorClass
)

type state int

const (
	// pre game phase
	PregameState state = iota
	// day phases
	DiscussionState
	VoteState
	// night phases
	MafiaState
	DoctorState
	DetectiveState
)

type player struct {
	game      *game
	name      string
	named     bool
	class     class
	alive     bool
	connected bool

	connection *websocket.Conn
}

type action interface {
	Execute(g *game)
}

type game struct {
	name    string
	players []*player
	turn    int
	state   state
}

func (g *game) unstartedSerializedStatement() []string {
	var playerNames []string

	for _, p := range g.players {
		if p.named {
			playerNames = append(playerNames, p.name)
		}
	}

	allNames := strings.Join(playerNames, ", ")

	return []string{"Lobby Code: " + g.name, fmt.Sprintf("Players: %s", allNames)}
}

func (g *game) addPlayer(c *websocket.Conn) (*player, *game) {
	p := &player{
		game:       g,
		named:      false,
		alive:      true,
		connected:  true,
		connection: c,
	}

	g.players = append(g.players, p)

	return p, g
}

func (g *game) namePlayer(p *player, name string) {
	p.name = name
	p.named = true

	// broadcast new state to all connected players
	for _, pp := range g.players {
		if pp.connected && p.named {
			o := options{
				Statements: g.unstartedSerializedStatement(),
				Options:    []string{"Start"},
				State:      "select",
			}

			sendToConn(o, pp.connection)
		}
	}
}

func (g *game) start() {
	ticker := time.NewTicker(time.Second * 60)
	for {
		<-ticker.C
	}
}

func (g *game) processAction(a action) {
	a.Execute(g)
}
