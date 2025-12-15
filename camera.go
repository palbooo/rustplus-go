package rustplus

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"sync"

	pb "github.com/palbooo/rustplus-go/proto"
)

// CameraType represents the type of camera
type CameraType int

const (
	CameraTypeUnknown CameraType = iota
	CameraTypeCCTV
	CameraTypePTZCCTV
	CameraTypeDrone
	CameraTypeAutoTurret
)

// CameraButtons represents camera control buttons
type CameraButtons int32

const (
	ButtonsNone          CameraButtons = 0
	ButtonsForward       CameraButtons = 2
	ButtonsBackward      CameraButtons = 4
	ButtonsLeft          CameraButtons = 8
	ButtonsRight         CameraButtons = 16
	ButtonsJump          CameraButtons = 32
	ButtonsDuck          CameraButtons = 64
	ButtonsSprint        CameraButtons = 128
	ButtonsUse           CameraButtons = 256
	ButtonsFirePrimary   CameraButtons = 1024
	ButtonsFireSecondary CameraButtons = 2048
	ButtonsReload        CameraButtons = 8192
	ButtonsFireThird     CameraButtons = 134217728
)

// CameraEventType represents camera event types
type CameraEventType int

const (
	CameraEventSubscribing CameraEventType = iota
	CameraEventSubscribed
	CameraEventUnsubscribing
	CameraEventUnsubscribed
	CameraEventRender
)

// CameraEvent represents a camera event
type CameraEvent struct {
	Type  CameraEventType
	Image *image.RGBA
}

// Camera handles camera subscriptions and rendering
type Camera struct {
	rustplus            *RustPlus
	identifier          string
	cameraType          CameraType
	playerId            string
	playerToken         int32
	isSubscribed        bool
	cameraRays          []*pb.AppCameraRays
	cameraSubscribeInfo *pb.AppCameraInfo
	mu                  sync.RWMutex
	eventHandlers       []func(CameraEvent)
	stopHandler         chan struct{}
}

// NewCamera creates a new Camera instance
func NewCamera(rustplus *RustPlus) *Camera {
	c := &Camera{
		rustplus:      rustplus,
		cameraRays:    make([]*pb.AppCameraRays, 0),
		eventHandlers: make([]func(CameraEvent), 0),
	}

	// Register message handler
	rustplus.On(func(event Event) {
		if event.Type == EventMessage && c.isSubscribed && event.Message.Broadcast != nil && event.Message.Broadcast.CameraRays != nil {
			c.handleCameraRays(event.Message.Broadcast.CameraRays)
		}
		if event.Type == EventDisconnected && c.isSubscribed {
			c.Unsubscribe()
		}
	})

	return c
}

// On registers a camera event handler
func (c *Camera) On(handler func(CameraEvent)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eventHandlers = append(c.eventHandlers, handler)
}

// emit sends an event to all registered handlers
func (c *Camera) emit(event CameraEvent) {
	c.mu.RLock()
	handlers := make([]func(CameraEvent), len(c.eventHandlers))
	copy(handlers, c.eventHandlers)
	c.mu.RUnlock()

	for _, handler := range handlers {
		handler(event)
	}
}

// Subscribe subscribes to a camera
func (c *Camera) Subscribe(playerId string, playerToken int32, identifier string) error {
	c.emit(CameraEvent{Type: CameraEventSubscribing})

	response, err := c.rustplus.CameraSubscribe(playerId, playerToken, identifier, true)
	if err != nil {
		return err
	}

	if GetAppResponseError(response) != NoError {
		return errors.New("failed to subscribe to camera")
	}

	c.mu.Lock()
	c.identifier = identifier
	c.playerId = playerId
	c.playerToken = playerToken
	c.isSubscribed = true
	c.cameraRays = make([]*pb.AppCameraRays, 0)
	c.cameraSubscribeInfo = response.CameraSubscribeInfo
	c.cameraType = getCameraType(response.CameraSubscribeInfo.ControlFlags)
	c.mu.Unlock()

	c.emit(CameraEvent{Type: CameraEventSubscribed})
	return nil
}

// Unsubscribe unsubscribes from the camera
func (c *Camera) Unsubscribe() error {
	c.emit(CameraEvent{Type: CameraEventUnsubscribing})

	if c.rustplus.IsConnected() {
		c.rustplus.CameraUnsubscribe(c.playerId, c.playerToken, true)
	}

	c.mu.Lock()
	c.identifier = ""
	c.playerId = ""
	c.playerToken = 0
	c.isSubscribed = false
	c.cameraRays = make([]*pb.AppCameraRays, 0)
	c.cameraSubscribeInfo = nil
	c.cameraType = CameraTypeUnknown
	c.mu.Unlock()

	c.emit(CameraEvent{Type: CameraEventUnsubscribed})
	return nil
}

// IsSubscribed returns whether subscribed to a camera
func (c *Camera) IsSubscribed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isSubscribed
}

// GetCameraType returns the camera type
func (c *Camera) GetCameraType() CameraType {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cameraType
}

// Zoom zooms a PTZ CCTV camera
func (c *Camera) Zoom() error {
	c.mu.RLock()
	subscribed := c.isSubscribed
	cType := c.cameraType
	playerId := c.playerId
	playerToken := c.playerToken
	c.mu.RUnlock()

	if !subscribed || cType != CameraTypePTZCCTV {
		return errors.New("not subscribed to PTZ CCTV camera")
	}

	// Press left mouse button to zoom
	c.rustplus.CameraInput(playerId, playerToken, int32(ButtonsFirePrimary), 0, 0, true)
	// Release button
	c.rustplus.CameraInput(playerId, playerToken, int32(ButtonsNone), 0, 0, true)

	return nil
}

// Shoot shoots an auto turret
func (c *Camera) Shoot() error {
	c.mu.RLock()
	subscribed := c.isSubscribed
	cType := c.cameraType
	playerId := c.playerId
	playerToken := c.playerToken
	c.mu.RUnlock()

	if !subscribed || cType != CameraTypeAutoTurret {
		return errors.New("not subscribed to auto turret")
	}

	// Press left mouse button to shoot
	c.rustplus.CameraInput(playerId, playerToken, int32(ButtonsFirePrimary), 0, 0, true)
	// Release button
	c.rustplus.CameraInput(playerId, playerToken, int32(ButtonsNone), 0, 0, true)

	return nil
}

// Reload reloads an auto turret
func (c *Camera) Reload() error {
	c.mu.RLock()
	subscribed := c.isSubscribed
	cType := c.cameraType
	playerId := c.playerId
	playerToken := c.playerToken
	c.mu.RUnlock()

	if !subscribed || cType != CameraTypeAutoTurret {
		return errors.New("not subscribed to auto turret")
	}

	// Press reload button
	c.rustplus.CameraInput(playerId, playerToken, int32(ButtonsReload), 0, 0, true)
	// Release button
	c.rustplus.CameraInput(playerId, playerToken, int32(ButtonsNone), 0, 0, true)

	return nil
}

// handleCameraRays handles incoming camera rays
func (c *Camera) handleCameraRays(rays *pb.AppCameraRays) {
	c.mu.Lock()
	c.cameraRays = append(c.cameraRays, rays)

	// Keep only last 10 frames
	if len(c.cameraRays) > 10 {
		c.cameraRays = c.cameraRays[1:]

		// Render to image
		img := c.renderCameraFrame()
		c.mu.Unlock()

		if img != nil {
			c.emit(CameraEvent{Type: CameraEventRender, Image: img})
		}
	} else {
		c.mu.Unlock()
	}
}

// renderCameraFrame renders camera rays to an image
func (c *Camera) renderCameraFrame() *image.RGBA {
	if c.cameraSubscribeInfo == nil || len(c.cameraRays) == 0 {
		return nil
	}

	width := int(c.cameraSubscribeInfo.Width)
	height := int(c.cameraSubscribeInfo.Height)
	frames := c.cameraRays

	// Create sample position buffer
	samplePositionBuffer := make([]int16, width*height*2)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := (y*width + x) * 2
			samplePositionBuffer[idx] = int16(x)
			samplePositionBuffer[idx+1] = int16(y)
		}
	}

	// Shuffle using IndexGenerator
	ig := newIndexGenerator(1337)
	for R := width*height - 1; R >= 1; R-- {
		C := 2 * R
		I := 2 * ig.nextInt(R+1)

		P := samplePositionBuffer[C]
		k := samplePositionBuffer[C+1]
		A := samplePositionBuffer[I]
		F := samplePositionBuffer[I+1]

		samplePositionBuffer[I] = P
		samplePositionBuffer[I+1] = k
		samplePositionBuffer[C] = A
		samplePositionBuffer[C+1] = F
	}

	// Create output buffer
	output := make([][3]float32, width*height)

	// Process each frame
	for _, frame := range frames {
		sampleOffset := int(frame.SampleOffset) * 2
		dataPointer := 0
		rayLookback := make([][3]int, 64)
		rayData := frame.RayData

		for dataPointer < len(rayData)-1 {
			var t, r, i int
			n := int(rayData[dataPointer])
			dataPointer++

			// Ray decoding logic (ported from TypeScript)
			if n == 255 {
				if dataPointer+2 >= len(rayData) {
					break
				}
				l := int(rayData[dataPointer])
				dataPointer++
				o := int(rayData[dataPointer])
				dataPointer++
				s := int(rayData[dataPointer])
				dataPointer++

				t = (l << 2) | (o >> 6)
				r = o & 63
				i = s
				u := (3*(t/128) + 5*(r/16) + 7*i) & 63
				rayLookback[u] = [3]int{t, r, i}
			} else {
				c := n & 192
				switch c {
				case 0:
					h := n & 63
					y := rayLookback[h]
					t, r, i = y[0], y[1], y[2]
				case 64:
					if dataPointer >= len(rayData) {
						break
					}
					p := n & 63
					v := rayLookback[p]
					b, w, h := v[0], v[1], v[2]
					g := int(rayData[dataPointer])
					dataPointer++
					t = b + ((g >> 3) - 15)
					r = w + ((g & 7) - 3)
					i = h
				case 128:
					if dataPointer >= len(rayData) {
						break
					}
					R := n & 63
					C := rayLookback[R]
					I, P, k := C[0], C[1], C[2]
					t = I + (int(rayData[dataPointer]) - 127)
					dataPointer++
					r = P
					i = k
				default:
					if dataPointer+1 >= len(rayData) {
						break
					}
					A := int(rayData[dataPointer])
					dataPointer++
					F := int(rayData[dataPointer])
					dataPointer++
					t = (A << 2) | (F >> 6)
					r = F & 63
					i = n & 63
					D := (3*(t/128) + 5*(r/16) + 7*i) & 63
					rayLookback[D] = [3]int{t, r, i}
				}
			}

			sampleOffset %= 2 * width * height
			if sampleOffset+1 < len(samplePositionBuffer) {
				index := int(samplePositionBuffer[sampleOffset]) + int(samplePositionBuffer[sampleOffset+1])*width
				sampleOffset += 2
				if index >= 0 && index < len(output) {
					output[index] = [3]float32{float32(t) / 1023.0, float32(r) / 63.0, float32(i)}
				}
			}
		}
	}

	// Color palette
	colours := [][3]float32{
		{0.5, 0.5, 0.5}, {0.8, 0.7, 0.7}, {0.3, 0.7, 1.0}, {0.6, 0.6, 0.6},
		{0.7, 0.7, 0.7}, {0.8, 0.6, 0.4}, {1.0, 0.4, 0.4}, {1.0, 0.1, 0.1},
	}

	// Create image
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	for i, ray := range output {
		x := i % width
		y := height - 1 - (i / width)

		if ray[0] == 0 && ray[1] == 0 && ray[2] == 0 {
			continue
		}

		distance := ray[0]
		alignment := ray[1]
		material := int(ray[2])

		var targetColor color.RGBA
		if distance == 1.0 && alignment == 0 && material == 0 {
			targetColor = color.RGBA{208, 230, 252, 255}
		} else {
			if material >= 0 && material < len(colours) {
				col := colours[material]
				targetColor = color.RGBA{
					uint8(alignment * col[0] * 255),
					uint8(alignment * col[1] * 255),
					uint8(alignment * col[2] * 255),
					255,
				}
			}
		}

		img.SetRGBA(x, y, targetColor)
	}

	return img
}

// SaveImagePNG saves an image to a PNG file
func SaveImagePNG(img *image.RGBA, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return png.Encode(f, img)
}

// getCameraType determines camera type from control flags
func getCameraType(controlFlags int32) CameraType {
	switch controlFlags {
	case 0:
		return CameraTypeCCTV
	case 10:
		return CameraTypePTZCCTV
	case 7:
		return CameraTypeDrone
	case 58:
		return CameraTypeAutoTurret
	default:
		return CameraTypeUnknown
	}
}

// IndexGenerator for shuffling (ported from TypeScript)
type indexGenerator struct {
	state int32
}

func newIndexGenerator(seed int32) *indexGenerator {
	ig := &indexGenerator{state: seed}
	ig.nextState()
	return ig
}

func (ig *indexGenerator) nextInt(n int) int {
	t := int((int64(ig.nextState()) * int64(n)) / 4294967295)
	if t < 0 {
		t = n + t - 1
	}
	return t
}

func (ig *indexGenerator) nextState() int32 {
	e := ig.state
	t := e
	e = (e ^ (e << 13)) ^ (e >> 17)
	e = e ^ (e << 5)
	ig.state = e
	if t >= 0 {
		return t
	}
	return int32(uint32(4294967295) + uint32(t) - 1)
}

// Random number generator seeded for reproducibility
func init() {
	rand.Seed(1337)
}

// Type aliases for convenience
type AppBroadcast = pb.AppBroadcast
type AppCameraRays = pb.AppCameraRays
type AppCameraInfo = pb.AppCameraInfo
type AppEntityType = pb.AppEntityType
