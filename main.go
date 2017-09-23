package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	//"net"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

func handleWs(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Upgrade(w, r, w.Header(), 1024, 1024)
	if err != nil {
		http.Error(w, "Could not open websocket connection", http.StatusBadRequest)
	}

	go processWebsocketInteractions(conn)
}

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	http.Handle("/", http.FileServer(http.Dir("static")))
	http.HandleFunc("/ws", handleWs)
	err := http.ListenAndServe(":8090", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

type MessageType int

const (
	Enter MessageType = iota
	Select
	Reconnect
)

type options struct {
	Statements []string
	Options    []string
	State      string
	Cookie     string
}

type msg struct {
	Type   MessageType
	Choice string
}

var games = make(map[string]*game)
var players = make(map[string]*player)

func makeNewGame() *game {
	name := ""

	for i := 0; i < 4; i++ {
		name += string(rune(65 + rand.Intn(26)))
	}

	g := &game{
		name:              name,
		nightActions:      make(map[*player]action),
		votes:             make(map[*player]vote),
		namutex:           &sync.Mutex{},
		vmutex:            &sync.Mutex{},
		players:           []*player{},
		state:             PregameState,
		turn:              0,
		allActionsDone:    make(chan int),
		allVotesCast:      make(chan int),
		allSkipDiscussion: make(chan int),
	}

	games[name] = g
	return g
}

func sendToConn(o options, conn *websocket.Conn) {
	if err := conn.WriteJSON(o); err != nil {
		fmt.Println(err)
	}
}

func processWebsocketInteractions(conn *websocket.Conn) {

	killed := false

	var g *game
	var p *player

	closeHandler := func(code int, text string) error {
		killed = true
		return nil
	}

	conn.SetCloseHandler(closeHandler)

	// handle incomming commands
	for !killed {
		m := msg{}

		err := conn.ReadJSON(&m)
		if err != nil {
			fmt.Println("Error reading json.", err)
			return
		}

		if g != nil && g.state == DoneState {
			p = nil
			g = nil
		}

		// not in game
		if g == nil {
			switch m.Type {
			case Enter:
				upperChoice := strings.ToUpper(m.Choice)

				if games[upperChoice] != nil && games[upperChoice].state == PregameState {
					g = games[upperChoice]
				} else {
					continue
				}
				p, g = g.addPlayer(conn)

				o := options{
					Statements: append(g.unstartedSerializedStatement(), "Pick a name"),
					State:      "enter",
				}

				sendToConn(o, conn)
			case Select:
				newgame := makeNewGame()
				p, g = newgame.addPlayer(conn)

				o := options{
					Statements: append(g.unstartedSerializedStatement(), "Pick a name"),
					State:      "enter",
				}
				sendToConn(o, conn)
			case Reconnect:
				if players[m.Choice] != nil {
					p = players[m.Choice]
					g = p.game
				}
			}
		} else {
			// in game

			switch m.Type {
			case Enter:
				if !p.named {
					g.namePlayer(p, m.Choice)
				} else {

				}
			case Select:
				if g.state == PregameState && m.Choice == "Start" && g.namedAndConnectedPlayers() >= 5 {
					go g.run()
				}

				if g.state == NightState || g.state == VoteState || g.state == DiscussionState {
					g.processAction(p, m.Choice)
				}
			}
		}
	}
}
