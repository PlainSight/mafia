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
	// post game state
	DoneState
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

type vote struct {
	actor  *player
	target *player
}

type game struct {
	name    string
	players []*player
	turn    int
	state   state

	nightActions map[*player]action
	namutex      *sync.Mutex

	votes  map[*player]vote
	vmutex *sync.Mutex

	skips int

	allActionsDone    chan int
	allVotesCast      chan int
	allSkipDiscussion chan int
}

func (g *game) addNightAction(a action) {
	g.namutex.Lock()
	g.nightActions[a.actor] = a

	waitingForActions := false
	for _, p := range g.players {
		if p.alive && p.class != CitizenClass {
			if _, ok := g.nightActions[p]; !ok {
				waitingForActions = true
			}
		}
	}
	g.namutex.Unlock()

	if !waitingForActions {
		g.allActionsDone <- 0
	}
}

func (g *game) addVote(v vote) {
	g.vmutex.Lock()
	g.votes[v.actor] = v

	waitingForVotes := false
	for _, p := range g.players {
		if p.alive {
			if _, ok := g.votes[p]; !ok {
				waitingForVotes = true
			}
		}
	}
	g.vmutex.Unlock()

	if !waitingForVotes {
		g.allVotesCast <- 0
	}
}

func (g *game) skipDiscussion() {
	g.skips++

	if len(g.alivePlayerList()) == g.skips {
		g.allSkipDiscussion <- 0
	}
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

	statements := []string{"Lobby Code: " + g.name, fmt.Sprintf("Players: %s", allNames)}

	if len(allNames) < 5 {
		statements = append(statements, "Require at least 5 players to start")
	}

	return statements
}

func (g *game) alivePlayerSansSelfList(self *player) []string {
	var alivePlayers []string

	for _, p := range g.players {
		if p.alive && p != self {
			alivePlayers = append(alivePlayers, p.name)
		}
	}

	return alivePlayers
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

func (g *game) gameIsOver() bool {
	mafiaCount := 0
	goodGuyCount := 0

	for _, p := range g.players {
		if p.alive {
			if p.class == MafiaClass {
				mafiaCount++
			} else {
				goodGuyCount++
			}
		}
	}
	// mafia won
	if mafiaCount >= goodGuyCount {
		return true
	}
	// goodies won
	if mafiaCount == 0 {
		return true
	}
	return false
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

func (p *player) sendThanksForSkippingState() {
	o := options{
		Statements: append(p.game.runningSerializedStatement(), "Time: Discussion-time", "Voted to skip"),
		State:      "info",
	}

	sendToConn(o, p.connection)
}

func (g *game) broadcastStartingState() {
	for _, p := range g.players {
		if p.connected {
			o := options{
				Statements: append(g.runningSerializedStatement(),
					fmt.Sprintf("You are a %s", p.class.toString()),
					"Night-time will begin in 10 seconds, please remember which role your character is"),
				State: "info",
			}

			sendToConn(o, p.connection)
		}
	}
}

func (g *game) broadcastDiscussionState() {
	for _, p := range g.players {
		if p.connected {
			var o options

			if p.alive {
				o = options{
					Statements: append(g.runningSerializedStatement(), "Time: Discussion-time"),
					Options:    []string{"Skip"},
					State:      "select",
				}

			} else {
				o = options{
					Statements: append(g.runningSerializedStatement(), "Time: Discussion-time"),
					State:      "info",
				}
			}

			sendToConn(o, p.connection)
		}
	}
}

func (g *game) broadcastNightState() {
	for _, p := range g.players {
		if p.connected {
			var o options

			if p.alive {
				o = options{
					Statements: append(g.runningSerializedStatement(), "Time: Night-time", p.class.actionString()),
					Options:    g.alivePlayerSansSelfList(p),
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
			var o options

			if p.alive {
				o = options{
					Statements: append(g.runningSerializedStatement(), "Time: Vote-time", "Vote to lynch someone"),
					Options:    g.alivePlayerSansSelfList(p),
					State:      "select",
				}
			} else {
				o = options{
					Statements: append(g.runningSerializedStatement(), "Time: Vote-time"),
					State:      "info",
				}
			}

			sendToConn(o, p.connection)
		}
	}
}

func (g *game) broadcastVoteResults(lynched *player) {
	result := "no one was lynched"
	if lynched != nil {
		result = fmt.Sprintf("%s was lynched after majority vote", lynched.name)
	}

	for _, p := range g.players {
		if p.connected {
			o := options{
				Statements: append(g.runningSerializedStatement(), "Time: Vote-time", result),
				State:      "info",
			}

			sendToConn(o, p.connection)
		}
	}
}

func (g *game) broadcastFinishState() {
	mafiaWin := false
	var mafiaNames []string
	for _, p := range g.players {
		if p.alive && p.class == MafiaClass {
			mafiaWin = true
		}
		if p.class == MafiaClass {
			mafiaNames = append(mafiaNames, p.name)
		}
	}

	statement := fmt.Sprintf("All the mafia (%s) are dead", strings.Join(mafiaNames, ", "))

	if mafiaWin {
		statement = fmt.Sprintf("The mafia (%s) have won", strings.Join(mafiaNames, ", "))
	}

	for _, p := range g.players {
		if p.connected {
			o := options{
				Statements: []string{statement},
				State:      "info",
			}

			sendToConn(o, p.connection)
		}
	}
}

func (g *game) broadcastResetState() {
	for _, p := range g.players {
		if p.connected {
			o := options{
				Statements: []string{"Create or join a game", "Enter the room code to join a game"},
				Options:    []string{"Create a game"},
				State:      "null",
			}

			sendToConn(o, p.connection)
		}
	}
}

func (g *game) broadcastNightResults(dead *player, investigated *player) {
	result := "during the night time no one died"
	if dead != nil {
		result = fmt.Sprintf("during the night time %s died", dead.name)
	}

	detected := ""

	if investigated != nil {
		detected = fmt.Sprintf("%s is not mafia", investigated.name)
		if investigated.class == MafiaClass {
			detected = fmt.Sprintf("%s is mafia", investigated.name)
		}
	}

	sments := append(g.runningSerializedStatement(), "Time: Morning-time", result)

	for _, p := range g.players {
		if p.connected {
			var o options

			if p.class == DetectiveClass && p.alive {
				o = options{
					Statements: append(sments, detected),
					State:      "info",
				}
			} else {
				o = options{
					Statements: sments,
					State:      "info",
				}
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

	time.Sleep(time.Second * 10)

	nextState := NightState

	for !g.gameIsOver() {
		if nextState == NightState {
			g.nightActions = make(map[*player]action)
			g.state = nextState
			g.broadcastNightState()

			// wait for all actions

			<-g.allActionsDone

			// calculate action effects
			dead, investigated := g.calculateNight()

			g.broadcastNightResults(dead, investigated)

			<-time.Tick(time.Second * 10)

			nextState = DiscussionState
		} else {
			// go to discusson

			g.skips = 0
			g.state = DiscussionState
			g.broadcastDiscussionState()

			select {
			case <-time.After(time.Second * 30):
			case <-g.allSkipDiscussion:
			}

			g.votes = make(map[*player]vote)
			g.state = VoteState
			g.broadcastVoteState()

			// wait for all votes

			select {
			case <-time.After(time.Second * 60):
			case <-g.allVotesCast:
			}

			// calculate vote effects
			lynched := g.calculateLynched()

			g.broadcastVoteResults(lynched)

			<-time.Tick(time.Second * 10)

			nextState = NightState
		}
	}

	g.broadcastFinishState()

	g.state = DoneState

	<-time.After(time.Second * 15)

	g.broadcastResetState()

	delete(games, g.name)
}

func (g *game) calculateLynched() *player {
	majorityNumber := (len(g.alivePlayerList()) / 2) + 1

	for _, p := range g.players {
		votes := 0

		for _, v := range g.votes {
			if v.target == p {
				votes++
			}
		}

		if votes >= majorityNumber {
			p.alive = false
			return p
		}
	}

	return nil
}

// returns dead, investigated
func (g *game) calculateNight() (*player, *player) {
	var dead, saved, investigated *player

	for _, a := range g.nightActions {
		switch a.actionType {
		case Kill:
			dead = a.target
		case Save:
			saved = a.target
		case Investigate:
			investigated = a.target
		}
	}

	if saved == dead {
		dead = nil
	}

	if dead != nil {
		dead.alive = false
	}

	return dead, investigated
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
	switch g.state {
	case NightState:
		p.sendThanksForActingState()

		a := action{
			actor:      p,
			target:     g.nameToPlayer(choice),
			actionType: p.class.toActionType(),
		}

		g.addNightAction(a)
	case DiscussionState:
		g.skipDiscussion()
		p.sendThanksForSkippingState()
	case VoteState:
		p.sendThanksForVotingState()

		v := vote{
			actor:  p,
			target: g.nameToPlayer(choice),
		}

		g.addVote(v)
	}
}
