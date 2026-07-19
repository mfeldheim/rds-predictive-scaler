package api

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"net/http"
	"sync"
	"time"

	"predictive-rds-scaler/types"
)

type Server struct {
	logger              *zerolog.Logger
	conf                *types.Config
	broadcast           chan types.Broadcast
	websocketClients    map[*websocket.Conn]bool
	websocketClientsMux sync.Mutex
	waitGroup           *sync.WaitGroup
	shutdownChannel     chan struct{}

	onClientConnect  func() []types.Broadcast
	onStartPatchMode func()
	onStopPatchMode  func()
}

func New(conf *types.Config, logger *zerolog.Logger, channel chan types.Broadcast) *Server {
	return &Server{
		logger:           logger,
		conf:             conf,
		broadcast:        channel,
		websocketClients: make(map[*websocket.Conn]bool),
		waitGroup:        &sync.WaitGroup{},
		shutdownChannel:  make(chan struct{}),
	}
}

func (api *Server) Serve(port uint) error {
	r := mux.NewRouter()
	newConnectionCh := make(chan *websocket.Conn)

	api.waitGroup.Add(1)
	go api.websocketManageConnections(newConnectionCh)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		EnableCompression: true,
	}

	r.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			api.logger.Error().Err(err).Msg("Error upgrading connection")
			return
		}

		api.logger.Info().Msgf("Client connected: %s", conn.RemoteAddr())
		newConnectionCh <- conn
	})

	r.PathPrefix("/").Handler(http.StripPrefix("/", http.FileServer(http.Dir("ui/build"))))

	api.logger.Info().Msgf("Listening on port %d", port)
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), r)
	if err != nil {
		return err
	}

	<-api.shutdownChannel
	api.waitGroup.Wait()
	return nil
}

func (api *Server) websocketManageConnections(newConnectionCh chan *websocket.Conn) {
	defer api.waitGroup.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-api.shutdownChannel:
			api.cleanupAndReturn()
			return
		case broadcast := <-api.broadcast:
			jsonData, err := json.Marshal(broadcast)
			if err != nil {
				api.logger.Error().Err(err).Msg("Error marshaling Broadcast")
				continue
			}
			api.websocketBroadcastByteSequence(jsonData)
		case conn := <-newConnectionCh:
			api.websocketClientInit(conn)
			go api.websocketListen(conn)
		}
	}
}

func (api *Server) cleanupAndReturn() {
	api.websocketClientsMux.Lock()
	defer api.websocketClientsMux.Unlock()

	for conn := range api.websocketClients {
		api.websocketClientDisconnect(conn)
	}
}

func (api *Server) websocketBroadcastByteSequence(seq []byte) {
	api.websocketClientsMux.Lock()
	defer api.websocketClientsMux.Unlock()

	for conn := range api.websocketClients {
		api.websocketSendByteSequence(seq, conn)
	}
}

// Send compressed data over WebSocket
func (api *Server) websocketSendByteSequence(seq []byte, conn *websocket.Conn) {
	if err := conn.WriteMessage(websocket.TextMessage, seq); err != nil {
		api.logger.Error().Err(err).Msg("Failed to write compressed message")
		api.websocketClientDisconnect(conn)
	}
}

func (api *Server) websocketClientDisconnect(conn *websocket.Conn) {
	if err := conn.Close(); err != nil {
		api.logger.Error().Err(err).Msg("Error closing connection")
	}
	api.websocketClientsMux.Lock()
	delete(api.websocketClients, conn)
	api.websocketClientsMux.Unlock()
}

func (api *Server) websocketClientInit(conn *websocket.Conn) {
	api.websocketClientsMux.Lock()
	conn.EnableWriteCompression(true)
	api.websocketClients[conn] = true
	api.websocketClientsMux.Unlock()

	for _, message := range api.onClientConnect() {
		jsonData, err := json.Marshal(message)
		if err != nil {
			api.logger.Error().Err(err).Msg("Error marshaling message from onClientConnect()")
			return
		}
		api.websocketSendByteSequence(jsonData, conn)
	}
}

func (api *Server) OnClientConnect(f func() []types.Broadcast) {
	api.onClientConnect = f
}

func (api *Server) OnPatchAction(onStart func(), onStop func()) {
	api.onStartPatchMode = onStart
	api.onStopPatchMode = onStop
}

func (api *Server) websocketListen(conn *websocket.Conn) {
	defer func() {
		api.websocketClientDisconnect(conn)
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				api.logger.Error().Err(err).Msg("Error reading WebSocket message")
			}
			break
		}
		api.handleIncomingMessage(message)
	}
}

func (api *Server) handleIncomingMessage(message []byte) {
	var receivedData types.Broadcast
	err := json.Unmarshal(message, &receivedData)
	if err != nil {
		api.logger.Error().Err(err).Msg("Error unmarshalling WebSocket message")
		return
	}

	switch receivedData.MessageType {
	case "conf_update":
		conf, ok := receivedData.Data.(types.Config)
		if !ok {
			api.logger.Error().Msg("Failed to cast data to conf")
			return
		}
		api.conf = &conf
		api.logger.Info().Msgf("Received configuration update: %+v", api.conf)

	case "start_patch_mode":
		api.logger.Info().Msg("Received start_patch_mode command")
		if api.onStartPatchMode != nil {
			go api.onStartPatchMode()
		}

	case "stop_patch_mode":
		api.logger.Info().Msg("Received stop_patch_mode command")
		if api.onStopPatchMode != nil {
			go api.onStopPatchMode()
		}

	default:
		api.logger.Warn().Msg("Received an unsupported message type")
	}
}

func (api *Server) Stop() {
	api.logger.Info().Msg("Stopping API server")
	close(api.shutdownChannel)
}
