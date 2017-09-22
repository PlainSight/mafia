package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"math/rand"
	"strings"
	"sync"
	"time"
)

type class int

const (
	CitizenClass class = iota
	MafiaClass
	DetectiveClass
	DoctorClass
)

func (c class) toString() string {
	switch c {
	case CitizenClass:
		return "Citizen"
	case MafiaClass:
		return "Mafia"
	case DetectiveClass:
		return "Detective"
	case DoctorClass:
		return "Doctor"
	default:
		return ""
	}
}

func (c class) toActionType() actionType {
	switch c {
	case CitizenClass:
		return Herping
	case MafiaClass:
		return Kill
	case DetectiveClass:
		return Investigate
	case DoctorClass:
		return Save
	default:
		return Herping
	}
}

func (c class) actionString() string {
	switch c {
	case CitizenClass:
		return "Please pretend to be doing something useful"
	case MafiaClass:
		return "Please murder someone"
	case DetectiveClass:
		return "Please investigate someone"
	case DoctorClass:
		return "Please save someone"
	default:
		return ""
	}
}

type state int

const (
	// pre game phase
	PregameState state = iota
	StartingState
	// day phases
	DiscussionState
	VoteState
	// night state
	NightState
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

type actionType int

const (
	Kill actionType = iota
	Save
	Investigate
	Herping
)

type action struct {
	actor      *player
	target     *player
	actionType actionType
}

type game struct {
	name    string
	players []*player
	turn    int
	state   state

	nightActions map[*player]action
	namutex      *sync.Mutex
}

func (g *game) addNightAction(a action) bool {
	g.namutex.Lock()
	g.nightActions[a.actor] = a
	g.namutex.Unlock()
	return true
}

func (g *game) nameToPlayer(name string) *player {
	for _, p := range g.players {
		if p.name == name {
			return p
		}
	}
	return nil
}

func (g *game) unstartedSerializedStatement() []string {
	var playerNames []string

	for _, p := range g.players {
		if p.named && p.connected {
			playerNames = append(playerNames, p.name)
		}
	}

	allNames := strings.Join(playerNames, ", ")

	return []string{"Lobby Code: " + g.name, fmt.Sprintf("Players: %s", allNames)}
}

func (g *game) alivePlayerList() []string {
	var alivePlayers []string

	for _, p := range g.players {
		if p.alive {
			alivePlayers = append(alivePlayers, p.name)
		}
	}

	return alivePlayers
}

func (g *game) runningSerializedStatement() []string {
	var aliveNames, deadNames []string

	for _, p := range g.players {
		if p.alive {
			aliveNames = append(aliveNames, p.name)
		} else {
			deadNames = append(deadNames, p.name)
		}
	}

	allAliveNames, allDeadNames := strings.Join(aliveNames, ", "), strings.Join(deadNames, ", ")

	return []string{"Lobby Code: " + g.name, fmt.Sprintf("Alive: %s", allAliveNames), fmt.Sprintf("Dead: %s", allDeadNames)}
}

func (g *game) broadcastStartingState() {
	for _, p := range g.players {
		if p.connected {
			o := options{
				Statements: append(g.runningSerializedStatement(),
					fmt.Sprintf("You are a %s", p.class.toString()),
					"Night-time will begin in 20 seconds, please remember which role your character is"),
				State: "info",
			}

			sendToConn(o, p.connection)
		}
	}
}

func (g *game) broadcastDiscussionState() {
	for _, p := range g.players {
		if p.connected {
			o := options{
				Statements: g.runningSerializedStatement(),
				State:      "info",
			}

			sendToConn(o, p.connection)
		}
	}
}

func (p *player) sendThanksForActingState() {
	o := options{
		Statements: append(p.game.runningSerializedStatement(), "Time: Night-time"),
		State:      "info",
	}

	sendToConn(o, p.connection)
}

func (p *player) sendThanksForVotingState() {
	o := options{
		Statements: append(p.game.runningSerializedStatement(), "Time: Vote-time"),
		State:      "info",
	}

	sendToConn(o, p.connection)
}

func (g *game) broadcastNightState() {
	for _, p := range g.players {
		if p.connected {
			var o options

			if p.alive {
				o = options{
					Statements: append(g.runningSerializedStatement(), "Time: Night-time", p.class.actionString()),
					Options:    g.alivePlayerList(),
					State:      "select",
				}
			} else {
				o = options{
					Statements: append(g.runningSerializedStatement(), "Time: Night-time"),
					State:      "info",
				}
			}

			sendToConn(o, p.connection)
		}
	}
}

func (g *game) broadcastVoteState() {
	for _, p := range g.players {
		if p.connected {
			o := options{
				Statements: g.runningSerializedStatement(),
				State:      "info",
			}

			sendToConn(o, p.connection)
		}
	}
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
		if pp.connected && pp.named {
			o := options{
				Statements: g.unstartedSerializedStatement(),
				Options:    []string{"Start"},
				State:      "select",
			}

			sendToConn(o, pp.connection)
		}
	}
}

func (g *game) namedAndConnectedPlayers() int {
	count := 0

	for _, p := range g.players {
		if p.connected && p.named {
			count++
		}
	}

	return count
}

func (g *game) run() {
	g.state = StartingState
	var filteredPlayers []*player

	for _, p := range g.players {
		if p.named && p.connected {
			filteredPlayers = append(filteredPlayers, p)
		}
	}

	g.players = filteredPlayers

	// assign roles
	g.assignRoles()

	g.broadcastStartingState()

	time.Sleep(time.Second * 20)

	g.state = NightState
	g.broadcastNightState()
}

func (g *game) assignRoles() {
	// assign mafia
	numberOfMafiaToAssign := len(g.players) / 3

	pn := int32(len(g.players))

	for numberOfMafiaToAssign > 0 {
		index := rand.Int31n(pn)
		if g.players[index].class == CitizenClass {
			g.players[index].class = MafiaClass
			numberOfMafiaToAssign--
		}
	}

	// assign detective
	haveAssignedDetective := false

	for !haveAssignedDetective {
		index := rand.Int31n(pn)
		if g.players[index].class == CitizenClass {
			g.players[index].class = DetectiveClass
			haveAssignedDetective = true
		}
	}

	// assign doctor
	haveAssignedDoctor := false

	for !haveAssignedDoctor {
		index := rand.Int31n(pn)
		if g.players[index].class == CitizenClass {
			g.players[index].class = DoctorClass
			haveAssignedDoctor = true
		}
	}
}

func (g *game) processAction(p *player, choice string) {
	if g.state == NightState {
		p.sendThanksForActingState()

		a := action{
			actor:      p,
			target:     g.nameToPlayer(choice),
			actionType: p.class.toActionType(),
		}

		g.addNightAction(a)
	}
	if g.state == VoteState {
		p.sendThanksForVotingState()
	}
}
