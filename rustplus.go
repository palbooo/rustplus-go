package rustplus

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	pb "github.com/palbooo/rustplus-go/proto"
	"google.golang.org/protobuf/proto"
)

// AppResponseError represents errors from the Rust+ API
type AppResponseError int

const (
	NoError AppResponseError = iota
	Unknown
	ServerError
	Banned
	RateLimit
	NotFound
	WrongType
	NoTeam
	NoClan
	NoMap
	NoCamera
	NoPlayer
	AccessDenied
	PlayerOnline
	InvalidPlayerid
	InvalidId
	InvalidMotd
	TooManySubscribers
	NotEnabled
	MessageNotSent
)

// ConsumeTokensError represents token consumption errors
type ConsumeTokensError int

const (
	ConsumeNoError ConsumeTokensError = iota
	ConsumeUnknown
	NotEnoughConnectionTokens
	NotEnoughPlayerIdTokens
	WaitReplenishTimeout
)

// EventType represents different event types
type EventType int

const (
	EventConnecting EventType = iota
	EventConnected
	EventMessage
	EventRequest
	EventDisconnected
	EventError
)

// Event represents an event from the RustPlus client
type Event struct {
	Type       EventType
	Message    *pb.AppMessage
	Request    *pb.AppRequest
	Error      error
	WasHandled bool
}

// RustPlus is the main client for interacting with Rust+ API
type RustPlus struct {
	IP                string
	Port              string
	UseFacepunchProxy bool

	mu               sync.RWMutex
	ws               *websocket.Conn
	seq              uint32
	seqCallbacks     map[uint32]func(*pb.AppMessage)
	connectionTokens float64
	playerIdTokens   map[string]float64
	serverPairTokens float64
	stopReplenish    chan struct{}
	eventHandlers    []func(Event)
	ctx              context.Context
	cancel           context.CancelFunc
}

// Rate limiting constants
const (
	maxRequestsPerIP                   = 50.0
	requestsPerIPReplenishRate         = 15.0 // per second
	maxRequestsPerPlayerID             = 25.0
	requestsPerPlayerIDReplenishRate   = 3.0 // per second
	maxRequestsForServerPairing        = 5.0
	requestsForServerPairingReplenRate = 0.1 // per second
	replenishInterval                  = 1 * time.Second
)

// Request timeouts (milliseconds)
const (
	RequestGetInfoTimeout           = 10000 * time.Millisecond
	RequestGetTimeTimeout           = 10000 * time.Millisecond
	RequestGetMapTimeout            = 30000 * time.Millisecond
	RequestGetTeamInfoTimeout       = 10000 * time.Millisecond
	RequestGetTeamChatTimeout       = 10000 * time.Millisecond
	RequestSendTeamMessageTimeout   = 10000 * time.Millisecond
	RequestGetEntityInfoTimeout     = 10000 * time.Millisecond
	RequestSetEntityValueTimeout    = 10000 * time.Millisecond
	RequestCheckSubscriptionTimeout = 10000 * time.Millisecond
	RequestSetSubscriptionTimeout   = 10000 * time.Millisecond
	RequestGetMapMarkersTimeout     = 10000 * time.Millisecond
	RequestPromoteToLeaderTimeout   = 10000 * time.Millisecond
	RequestGetClanInfoTimeout       = 10000 * time.Millisecond
	RequestSetClanMotdTimeout       = 10000 * time.Millisecond
	RequestGetClanChatTimeout       = 10000 * time.Millisecond
	RequestSendClanMessageTimeout   = 10000 * time.Millisecond
	RequestGetNexusAuthTimeout      = 10000 * time.Millisecond
	RequestCameraSubscribeTimeout   = 10000 * time.Millisecond
	RequestCameraUnsubscribeTimeout = 10000 * time.Millisecond
	RequestCameraInputTimeout       = 10000 * time.Millisecond
)

// Token costs for different operations
const (
	TokenCostGetInfo           = 1.0
	TokenCostGetTime           = 1.0
	TokenCostGetMap            = 5.0
	TokenCostGetTeamInfo       = 1.0
	TokenCostGetTeamChat       = 1.0
	TokenCostSendTeamMessage   = 2.0
	TokenCostGetEntityInfo     = 1.0
	TokenCostSetEntityValue    = 1.0
	TokenCostCheckSubscription = 1.0
	TokenCostSetSubscription   = 1.0
	TokenCostGetMapMarkers     = 1.0
	TokenCostPromoteToLeader   = 1.0
	TokenCostGetClanInfo       = 1.0
	TokenCostSetClanMotd       = 1.0
	TokenCostGetClanChat       = 1.0
	TokenCostSendClanMessage   = 2.0
	TokenCostGetNexusAuth      = 1.0
	TokenCostCameraSubscribe   = 1.0
	TokenCostCameraUnsubscribe = 1.0
	TokenCostCameraInput       = 0.01
)

// NewRustPlus creates a new RustPlus client
func NewRustPlus(ip, port string, useFacepunchProxy bool) *RustPlus {
	ctx, cancel := context.WithCancel(context.Background())
	return &RustPlus{
		IP:                ip,
		Port:              port,
		UseFacepunchProxy: useFacepunchProxy,
		seqCallbacks:      make(map[uint32]func(*pb.AppMessage)),
		connectionTokens:  maxRequestsPerIP,
		playerIdTokens:    make(map[string]float64),
		serverPairTokens:  maxRequestsForServerPairing,
		eventHandlers:     make([]func(Event), 0),
		ctx:               ctx,
		cancel:            cancel,
	}
}

// On registers an event handler
func (r *RustPlus) On(handler func(Event)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventHandlers = append(r.eventHandlers, handler)
}

// emit sends an event to all registered handlers
func (r *RustPlus) emit(event Event) {
	r.mu.RLock()
	handlers := make([]func(Event), len(r.eventHandlers))
	copy(handlers, r.eventHandlers)
	r.mu.RUnlock()

	for _, handler := range handlers {
		handler(event)
	}
}

// getAddress returns the WebSocket address
func (r *RustPlus) getAddress() string {
	if r.UseFacepunchProxy {
		return fmt.Sprintf("wss://companion-rust.facepunch.com/game/%s/%s", r.IP, r.Port)
	}
	return fmt.Sprintf("ws://%s:%s", r.IP, r.Port)
}

// Connect establishes a WebSocket connection to the Rust server
func (r *RustPlus) Connect() error {
	r.mu.Lock()
	if r.ws != nil {
		r.mu.Unlock()
		r.Disconnect()
		r.mu.Lock()
	}
	r.mu.Unlock()

	r.emit(Event{Type: EventConnecting})

	addr := r.getAddress()
	u, err := url.Parse(addr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 45 * time.Second,
	}

	ws, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	r.mu.Lock()
	r.ws = ws
	r.mu.Unlock()

	r.emit(Event{Type: EventConnected})

	// Start token replenishment
	r.stopReplenish = make(chan struct{})
	go r.replenishTask()

	// Start message handler
	go r.handleMessages()

	return nil
}

// Disconnect closes the WebSocket connection
func (r *RustPlus) Disconnect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stopReplenish != nil {
		close(r.stopReplenish)
		r.stopReplenish = nil
	}

	if r.ws != nil {
		err := r.ws.Close()
		r.ws = nil
		r.emit(Event{Type: EventDisconnected})
		return err
	}

	return nil
}

// IsConnected returns whether the client is connected
func (r *RustPlus) IsConnected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ws != nil
}

// handleMessages processes incoming WebSocket messages
func (r *RustPlus) handleMessages() {
	for {
		r.mu.RLock()
		ws := r.ws
		r.mu.RUnlock()

		if ws == nil {
			return
		}

		_, data, err := ws.ReadMessage()
		if err != nil {
			r.emit(Event{Type: EventError, Error: err})
			r.Disconnect()
			return
		}

		appMessage := &pb.AppMessage{}
		if err := proto.Unmarshal(data, appMessage); err != nil {
			r.emit(Event{Type: EventError, Error: fmt.Errorf("failed to unmarshal message: %w", err)})
			continue
		}

		handled := false
		if appMessage.Response != nil {
			r.mu.Lock()
			callback, exists := r.seqCallbacks[appMessage.Response.Seq]
			if exists {
				delete(r.seqCallbacks, appMessage.Response.Seq)
				r.mu.Unlock()
				callback(appMessage)
				handled = true
			} else {
				r.mu.Unlock()
			}
		}

		r.emit(Event{Type: EventMessage, Message: appMessage, WasHandled: handled})
	}
}

// replenishTask replenishes tokens periodically
func (r *RustPlus) replenishTask() {
	ticker := time.NewTicker(replenishInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopReplenish:
			return
		case <-ticker.C:
			r.replenishTokens()
		}
	}
}

// replenishTokens replenishes all token buckets
func (r *RustPlus) replenishTokens() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Replenish connection tokens
	if r.connectionTokens < maxRequestsPerIP {
		r.connectionTokens = math.Min(r.connectionTokens+requestsPerIPReplenishRate, maxRequestsPerIP)
	}

	// Replenish player ID tokens
	for playerId := range r.playerIdTokens {
		if r.playerIdTokens[playerId] < maxRequestsPerPlayerID {
			r.playerIdTokens[playerId] = math.Min(
				r.playerIdTokens[playerId]+requestsPerPlayerIDReplenishRate,
				maxRequestsPerPlayerID,
			)
		}
	}

	// Replenish server pairing tokens
	if r.serverPairTokens < maxRequestsForServerPairing {
		r.serverPairTokens = math.Min(
			r.serverPairTokens+requestsForServerPairingReplenRate,
			maxRequestsForServerPairing,
		)
	}
}

// consumeTokens attempts to consume tokens for a request
func (r *RustPlus) consumeTokens(playerId string, tokens float64, waitForReplenish bool, timeout time.Duration) ConsumeTokensError {
	startTime := time.Now()

	r.mu.Lock()
	if _, exists := r.playerIdTokens[playerId]; !exists {
		r.playerIdTokens[playerId] = maxRequestsPerPlayerID
	}
	r.mu.Unlock()

	for {
		r.mu.Lock()
		hasEnough := r.connectionTokens >= tokens && r.playerIdTokens[playerId] >= tokens
		r.mu.Unlock()

		if hasEnough {
			r.mu.Lock()
			r.connectionTokens -= tokens
			r.playerIdTokens[playerId] -= tokens
			r.mu.Unlock()
			return ConsumeNoError
		}

		if !waitForReplenish {
			r.mu.RLock()
			defer r.mu.RUnlock()
			if r.connectionTokens < tokens {
				return NotEnoughConnectionTokens
			}
			if r.playerIdTokens[playerId] < tokens {
				return NotEnoughPlayerIdTokens
			}
			return ConsumeUnknown
		}

		if time.Since(startTime) >= timeout {
			return WaitReplenishTimeout
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// getNextSeq returns the next sequence number
func (r *RustPlus) getNextSeq() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.seq++
	for {
		if _, exists := r.seqCallbacks[r.seq]; !exists {
			return r.seq
		}
		r.seq++
	}
}

// SendRequest sends a request to the Rust server
func (r *RustPlus) SendRequest(req *pb.AppRequest, callback func(*pb.AppMessage)) error {
	r.mu.RLock()
	if r.ws == nil {
		r.mu.RUnlock()
		return errors.New("websocket is not connected")
	}
	r.mu.RUnlock()

	r.mu.Lock()
	r.seqCallbacks[req.Seq] = callback
	r.mu.Unlock()

	data, err := proto.Marshal(req)
	if err != nil {
		r.mu.Lock()
		delete(r.seqCallbacks, req.Seq)
		r.mu.Unlock()
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	r.mu.RLock()
	ws := r.ws
	r.mu.RUnlock()

	if err := ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
		r.mu.Lock()
		delete(r.seqCallbacks, req.Seq)
		r.mu.Unlock()
		return fmt.Errorf("failed to send request: %w", err)
	}

	r.emit(Event{Type: EventRequest, Request: req})
	return nil
}

// SendRequestAsync sends a request and waits for the response
func (r *RustPlus) SendRequestAsync(req *pb.AppRequest, timeout time.Duration) (*pb.AppResponse, error) {
	responseChan := make(chan *pb.AppResponse, 1)
	errorChan := make(chan error, 1)

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	callback := func(msg *pb.AppMessage) {
		if msg.Response != nil {
			responseChan <- msg.Response
		} else {
			errorChan <- errors.New("no response in message")
		}
	}

	if err := r.SendRequest(req, callback); err != nil {
		return nil, err
	}

	select {
	case response := <-responseChan:
		return response, nil
	case err := <-errorChan:
		return nil, err
	case <-timer.C:
		r.mu.Lock()
		delete(r.seqCallbacks, req.Seq)
		r.mu.Unlock()
		return nil, errors.New("request timeout")
	}
}

// GetAppResponseError translates an AppResponse error to AppResponseError enum
func GetAppResponseError(response *pb.AppResponse) AppResponseError {
	if response.Error == nil {
		return NoError
	}

	switch response.Error.Error {
	case "server_error":
		return ServerError
	case "banned":
		return Banned
	case "rate_limit":
		return RateLimit
	case "not_found":
		return NotFound
	case "wrong_type":
		return WrongType
	case "no_team":
		return NoTeam
	case "no_clan":
		return NoClan
	case "no_map":
		return NoMap
	case "no_camera":
		return NoCamera
	case "no_player":
		return NoPlayer
	case "access_denied":
		return AccessDenied
	case "player_online":
		return PlayerOnline
	case "invalid_playerid":
		return InvalidPlayerid
	case "invalid_id":
		return InvalidId
	case "invalid_motd":
		return InvalidMotd
	case "too_many_subscribers":
		return TooManySubscribers
	case "not_enabled":
		return NotEnabled
	case "message_not_sent":
		return MessageNotSent
	default:
		return Unknown
	}
}

// API Methods

// GetInfo gets server information
func (r *RustPlus) GetInfo(playerId string, playerToken int32, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostGetInfo, waitForReplenish, RequestGetInfoTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:         r.getNextSeq(),
		PlayerId:    stringToUint64(playerId),
		PlayerToken: playerToken,
		GetInfo:     &pb.AppEmpty{},
	}

	return r.SendRequestAsync(req, RequestGetInfoTimeout)
}

// GetTime gets server time
func (r *RustPlus) GetTime(playerId string, playerToken int32, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostGetTime, waitForReplenish, RequestGetTimeTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:         r.getNextSeq(),
		PlayerId:    stringToUint64(playerId),
		PlayerToken: playerToken,
		GetTime:     &pb.AppEmpty{},
	}

	return r.SendRequestAsync(req, RequestGetTimeTimeout)
}

// GetMap gets the server map
func (r *RustPlus) GetMap(playerId string, playerToken int32, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostGetMap, waitForReplenish, RequestGetMapTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:         r.getNextSeq(),
		PlayerId:    stringToUint64(playerId),
		PlayerToken: playerToken,
		GetMap:      &pb.AppEmpty{},
	}

	return r.SendRequestAsync(req, RequestGetMapTimeout)
}

// GetTeamInfo gets team information
func (r *RustPlus) GetTeamInfo(playerId string, playerToken int32, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostGetTeamInfo, waitForReplenish, RequestGetTeamInfoTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:         r.getNextSeq(),
		PlayerId:    stringToUint64(playerId),
		PlayerToken: playerToken,
		GetTeamInfo: &pb.AppEmpty{},
	}

	return r.SendRequestAsync(req, RequestGetTeamInfoTimeout)
}

// GetTeamChat gets team chat messages
func (r *RustPlus) GetTeamChat(playerId string, playerToken int32, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostGetTeamChat, waitForReplenish, RequestGetTeamChatTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:         r.getNextSeq(),
		PlayerId:    stringToUint64(playerId),
		PlayerToken: playerToken,
		GetTeamChat: &pb.AppEmpty{},
	}

	return r.SendRequestAsync(req, RequestGetTeamChatTimeout)
}

// SendTeamMessage sends a team message
func (r *RustPlus) SendTeamMessage(playerId string, playerToken int32, message string, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostSendTeamMessage, waitForReplenish, RequestSendTeamMessageTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:             r.getNextSeq(),
		PlayerId:        stringToUint64(playerId),
		PlayerToken:     playerToken,
		SendTeamMessage: &pb.AppSendMessage{Message: message},
	}

	return r.SendRequestAsync(req, RequestSendTeamMessageTimeout)
}

// GetEntityInfo gets entity information
func (r *RustPlus) GetEntityInfo(playerId string, playerToken int32, entityId uint32, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostGetEntityInfo, waitForReplenish, RequestGetEntityInfoTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:           r.getNextSeq(),
		PlayerId:      stringToUint64(playerId),
		PlayerToken:   playerToken,
		EntityId:      &entityId,
		GetEntityInfo: &pb.AppEmpty{},
	}

	return r.SendRequestAsync(req, RequestGetEntityInfoTimeout)
}

// SetEntityValue sets entity value (switch on/off)
func (r *RustPlus) SetEntityValue(playerId string, playerToken int32, entityId uint32, value bool, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostSetEntityValue, waitForReplenish, RequestSetEntityValueTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:            r.getNextSeq(),
		PlayerId:       stringToUint64(playerId),
		PlayerToken:    playerToken,
		EntityId:       &entityId,
		SetEntityValue: &pb.AppSetEntityValue{Value: value},
	}

	return r.SendRequestAsync(req, RequestSetEntityValueTimeout)
}

// CheckSubscription checks alarm subscription
func (r *RustPlus) CheckSubscription(playerId string, playerToken int32, entityId uint32, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostCheckSubscription, waitForReplenish, RequestCheckSubscriptionTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:               r.getNextSeq(),
		PlayerId:          stringToUint64(playerId),
		PlayerToken:       playerToken,
		EntityId:          &entityId,
		CheckSubscription: &pb.AppEmpty{},
	}

	return r.SendRequestAsync(req, RequestCheckSubscriptionTimeout)
}

// SetSubscription sets alarm subscription
func (r *RustPlus) SetSubscription(playerId string, playerToken int32, entityId uint32, value bool, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostSetSubscription, waitForReplenish, RequestSetSubscriptionTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:             r.getNextSeq(),
		PlayerId:        stringToUint64(playerId),
		PlayerToken:     playerToken,
		EntityId:        &entityId,
		SetSubscription: &pb.AppFlag{Value: &value},
	}

	return r.SendRequestAsync(req, RequestSetSubscriptionTimeout)
}

// GetMapMarkers gets map markers
func (r *RustPlus) GetMapMarkers(playerId string, playerToken int32, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostGetMapMarkers, waitForReplenish, RequestGetMapMarkersTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:           r.getNextSeq(),
		PlayerId:      stringToUint64(playerId),
		PlayerToken:   playerToken,
		GetMapMarkers: &pb.AppEmpty{},
	}

	return r.SendRequestAsync(req, RequestGetMapMarkersTimeout)
}

// PromoteToLeader promotes a team member to leader
func (r *RustPlus) PromoteToLeader(playerId string, playerToken int32, steamId string, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostPromoteToLeader, waitForReplenish, RequestPromoteToLeaderTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:             r.getNextSeq(),
		PlayerId:        stringToUint64(playerId),
		PlayerToken:     playerToken,
		PromoteToLeader: &pb.AppPromoteToLeader{SteamId: stringToUint64(steamId)},
	}

	return r.SendRequestAsync(req, RequestPromoteToLeaderTimeout)
}

// GetClanInfo gets clan information
func (r *RustPlus) GetClanInfo(playerId string, playerToken int32, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostGetClanInfo, waitForReplenish, RequestGetClanInfoTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:         r.getNextSeq(),
		PlayerId:    stringToUint64(playerId),
		PlayerToken: playerToken,
		GetClanInfo: &pb.AppEmpty{},
	}

	return r.SendRequestAsync(req, RequestGetClanInfoTimeout)
}

// SetClanMotd sets clan message of the day
func (r *RustPlus) SetClanMotd(playerId string, playerToken int32, message string, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostSetClanMotd, waitForReplenish, RequestSetClanMotdTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:         r.getNextSeq(),
		PlayerId:    stringToUint64(playerId),
		PlayerToken: playerToken,
		SetClanMotd: &pb.AppSendMessage{Message: message},
	}

	return r.SendRequestAsync(req, RequestSetClanMotdTimeout)
}

// GetClanChat gets clan chat messages
func (r *RustPlus) GetClanChat(playerId string, playerToken int32, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostGetClanChat, waitForReplenish, RequestGetClanChatTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:         r.getNextSeq(),
		PlayerId:    stringToUint64(playerId),
		PlayerToken: playerToken,
		GetClanChat: &pb.AppEmpty{},
	}

	return r.SendRequestAsync(req, RequestGetClanChatTimeout)
}

// SendClanMessage sends a clan message
func (r *RustPlus) SendClanMessage(playerId string, playerToken int32, message string, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostSendClanMessage, waitForReplenish, RequestSendClanMessageTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:             r.getNextSeq(),
		PlayerId:        stringToUint64(playerId),
		PlayerToken:     playerToken,
		SendClanMessage: &pb.AppSendMessage{Message: message},
	}

	return r.SendRequestAsync(req, RequestSendClanMessageTimeout)
}

// GetNexusAuth gets nexus authentication
func (r *RustPlus) GetNexusAuth(playerId string, playerToken int32, appKey string, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostGetNexusAuth, waitForReplenish, RequestGetNexusAuthTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:          r.getNextSeq(),
		PlayerId:     stringToUint64(playerId),
		PlayerToken:  playerToken,
		GetNexusAuth: &pb.AppGetNexusAuth{AppKey: appKey},
	}

	return r.SendRequestAsync(req, RequestGetNexusAuthTimeout)
}

// CameraSubscribe subscribes to a camera
func (r *RustPlus) CameraSubscribe(playerId string, playerToken int32, identifier string, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostCameraSubscribe, waitForReplenish, RequestCameraSubscribeTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:             r.getNextSeq(),
		PlayerId:        stringToUint64(playerId),
		PlayerToken:     playerToken,
		CameraSubscribe: &pb.AppCameraSubscribe{CameraId: identifier},
	}

	return r.SendRequestAsync(req, RequestCameraSubscribeTimeout)
}

// CameraUnsubscribe unsubscribes from a camera
func (r *RustPlus) CameraUnsubscribe(playerId string, playerToken int32, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostCameraUnsubscribe, waitForReplenish, RequestCameraUnsubscribeTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:               r.getNextSeq(),
		PlayerId:          stringToUint64(playerId),
		PlayerToken:       playerToken,
		CameraUnsubscribe: &pb.AppEmpty{},
	}

	return r.SendRequestAsync(req, RequestCameraUnsubscribeTimeout)
}

// CameraInput sends camera input
func (r *RustPlus) CameraInput(playerId string, playerToken int32, buttons int32, x, y float32, waitForReplenish bool) (*pb.AppResponse, error) {
	if err := r.consumeTokens(playerId, TokenCostCameraInput, waitForReplenish, RequestCameraInputTimeout); err != ConsumeNoError {
		return nil, fmt.Errorf("token error: %d", err)
	}

	req := &pb.AppRequest{
		Seq:         r.getNextSeq(),
		PlayerId:    stringToUint64(playerId),
		PlayerToken: playerToken,
		CameraInput: &pb.AppCameraInput{
			Buttons:    buttons,
			MouseDelta: &pb.Vector2{X: x, Y: y},
		},
	}

	return r.SendRequestAsync(req, RequestCameraInputTimeout)
}

// Helper functions

func stringToUint64(s string) uint64 {
	var result uint64
	fmt.Sscan(s, &result)
	return result
}

// Simple logger for debugging
func logError(format string, args ...interface{}) {
	log.Printf("[ERROR] "+format, args...)
}
