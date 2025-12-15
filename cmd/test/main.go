package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	rustplus "github.com/palbooo/rustplus-go"
	pb "github.com/palbooo/rustplus-go/proto"
)

var (
	ip         = flag.String("ip", "", "Rust server IP address")
	port       = flag.String("port", "", "Rust server app port")
	playerId   = flag.String("player-id", "", "Steam ID (64-bit)")
	token      = flag.Int("token", 0, "Player token")
	useProxy   = flag.Bool("proxy", false, "Use Facepunch proxy")
	entityId   = flag.Uint("entity-id", 0, "Entity ID for entity operations")
	cameraId   = flag.String("camera-id", "", "Camera ID for camera operations")
	message    = flag.String("message", "Hello from rustplus-go!", "Message to send")
	testAll    = flag.Bool("test-all", false, "Run all available tests")
	testBasic  = flag.Bool("test-basic", false, "Test basic operations (info, time, map)")
	testTeam   = flag.Bool("test-team", false, "Test team operations")
	testEntity = flag.Bool("test-entity", false, "Test entity operations")
	testCamera = flag.Bool("test-camera", false, "Test camera operations")
)

func main() {
	flag.Parse()

	// Validate required flags
	if *ip == "" || *port == "" || *playerId == "" || *token == 0 {
		fmt.Println("rustplus-go Test Script")
		fmt.Println("=======================")
		fmt.Println("\nRequired flags:")
		fmt.Println("  -ip string       : Rust server IP address")
		fmt.Println("  -port string     : Rust server app port")
		fmt.Println("  -player-id string: Steam ID (64-bit)")
		fmt.Println("  -token int       : Player token")
		fmt.Println("\nOptional flags:")
		fmt.Println("  -proxy           : Use Facepunch proxy (default: false)")
		fmt.Println("  -entity-id uint  : Entity ID for entity tests")
		fmt.Println("  -camera-id string: Camera ID for camera tests")
		fmt.Println("  -message string  : Message for chat tests")
		fmt.Println("\nTest flags:")
		fmt.Println("  -test-all        : Run all available tests")
		fmt.Println("  -test-basic      : Test basic operations")
		fmt.Println("  -test-team       : Test team operations")
		fmt.Println("  -test-entity     : Test entity operations")
		fmt.Println("  -test-camera     : Test camera operations")
		fmt.Println("\nExample:")
		fmt.Println("  ./rustplus-test -ip=192.168.1.100 -port=28082 \\")
		fmt.Println("    -player-id=76561198012345678 -token=123456 -test-all")
		os.Exit(1)
	}

	fmt.Println("RustPlus Go - Comprehensive Test Script")
	fmt.Println()

	// Create RustPlus client
	client := rustplus.NewRustPlus(*ip, *port, *useProxy)

	// Register event handlers
	client.On(func(event rustplus.Event) {
		switch event.Type {
		case rustplus.EventConnecting:
			fmt.Println("Connecting to Rust server...")
		case rustplus.EventConnected:
			fmt.Println("Connected to Rust server!")
		case rustplus.EventDisconnected:
			fmt.Println("Disconnected from Rust server")
		case rustplus.EventError:
			fmt.Printf("Error: %v\n", event.Error)
		case rustplus.EventMessage:
			if !event.WasHandled && event.Message.Broadcast != nil {
				fmt.Println("Received broadcast message")
				handleBroadcast(event.Message.Broadcast)
			}
		}
	})

	// Connect to the server
	if err := client.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect()

	// Wait for connection
	time.Sleep(2 * time.Second)

	// Determine which tests to run
	runBasic := *testAll || *testBasic
	runTeam := *testAll || *testTeam
	runEntity := *testAll || *testEntity || (*entityId > 0)
	runCamera := *testAll || *testCamera || (*cameraId != "")

	// If no specific tests, run basic tests
	if !runBasic && !runTeam && !runEntity && !runCamera {
		runBasic = true
	}

	// Run tests
	if runBasic {
		testBasicOperations(client, *playerId, int32(*token))
	}

	if runTeam {
		testTeamOperations(client, *playerId, int32(*token), *message)
	}

	if runEntity && *entityId > 0 {
		testEntityOperations(client, *playerId, int32(*token), uint32(*entityId))
	}

	if runCamera && *cameraId != "" {
		testCameraOperations(client, *playerId, int32(*token), *cameraId)
	}

	// Interactive mode
	fmt.Println("\nInteractive Mode")
	fmt.Println("\nCommands:")
	fmt.Println("  info       - Get server info")
	fmt.Println("  time       - Get server time")
	fmt.Println("  team       - Get team info")
	fmt.Println("  markers    - Get map markers")
	fmt.Println("  entity <id>- Get entity info")
	fmt.Println("  quit       - Exit")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		parts := strings.Fields(input)
		cmd := parts[0]

		switch cmd {
		case "quit", "exit", "q":
			fmt.Println("Goodbye!")
			return
		case "info":
			getServerInfo(client, *playerId, int32(*token))
		case "time":
			getServerTime(client, *playerId, int32(*token))
		case "team":
			getTeamInfo(client, *playerId, int32(*token))
		case "markers":
			getMapMarkers(client, *playerId, int32(*token))
		case "entity":
			if len(parts) < 2 {
				fmt.Println("Usage: entity <entity-id>")
				continue
			}
			var eid uint32
			fmt.Sscanf(parts[1], "%d", &eid)
			getEntityInfo(client, *playerId, int32(*token), eid)
		default:
			fmt.Println("Unknown command. Type 'quit' to exit.")
		}
	}
}

func testBasicOperations(client *rustplus.RustPlus, playerId string, token int32) {
	fmt.Println("\nBasic Operations Test")

	getServerInfo(client, playerId, token)
	time.Sleep(1 * time.Second)

	getServerTime(client, playerId, token)
	time.Sleep(1 * time.Second)

	getMapMarkers(client, playerId, token)
	time.Sleep(1 * time.Second)

	// Note: GetMap can be large, uncomment if needed
	// fmt.Println("\nGetting server map...")
	// resp, err := client.GetMap(playerId, token, true)
	// if err != nil {
	// 	fmt.Printf("Error: %v\n", err)
	// } else if resp.Map != nil {
	// 	fmt.Printf("Map size: %dx%d, Image size: %d bytes\n",
	// 		resp.Map.Width, resp.Map.Height, len(resp.Map.JpgImage))
	// }
}

func testTeamOperations(client *rustplus.RustPlus, playerId string, token int32, message string) {
	fmt.Println("\nTeam Operations Test")

	getTeamInfo(client, playerId, token)
	time.Sleep(1 * time.Second)

	fmt.Println("\nGetting team chat...")
	resp, err := client.GetTeamChat(playerId, token, true)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else if resp.TeamChat != nil {
		fmt.Printf("Team chat messages: %d\n", len(resp.TeamChat.Messages))
		for i, msg := range resp.TeamChat.Messages {
			if i >= 5 {
				break
			}
			fmt.Printf("[%s] %s\n", msg.Name, msg.Message)
		}
	}

	time.Sleep(1 * time.Second)

	fmt.Println("\nSending team message...")
	resp, err = client.SendTeamMessage(playerId, token, message, true)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		errCode := rustplus.GetAppResponseError(resp)
		if errCode == rustplus.NoError {
			fmt.Println("Message sent successfully!")
		} else {
			fmt.Printf("Error code: %d\n", errCode)
		}
	}
}

func testEntityOperations(client *rustplus.RustPlus, playerId string, token int32, entityId uint32) {
	fmt.Println("\nEntity Operations Test")

	getEntityInfo(client, playerId, token, entityId)
	time.Sleep(1 * time.Second)

	fmt.Printf("\nToggling entity %d...\n", entityId)
	resp, err := client.GetEntityInfo(playerId, token, entityId, true)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if resp.EntityInfo != nil && resp.EntityInfo.Payload != nil {
		currentValue := resp.EntityInfo.Payload.Value
		newValue := !currentValue
		fmt.Printf("Current value: %v, Setting to: %v\n", currentValue, newValue)

		resp, err = client.SetEntityValue(playerId, token, entityId, newValue, true)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			errCode := rustplus.GetAppResponseError(resp)
			if errCode == rustplus.NoError {
				fmt.Println("Entity value set successfully!")
			} else {
				fmt.Printf("Error code: %d\n", errCode)
			}
		}
	}
}

func testCameraOperations(client *rustplus.RustPlus, playerId string, token int32, cameraId string) {
	fmt.Println("\nCamera Operations Test")

	camera := rustplus.NewCamera(client)

	// Register camera event handler
	frameCount := 0
	camera.On(func(event rustplus.CameraEvent) {
		switch event.Type {
		case rustplus.CameraEventSubscribing:
			fmt.Println("Subscribing to camera...")
		case rustplus.CameraEventSubscribed:
			fmt.Printf("Subscribed! Camera type: %d\n", camera.GetCameraType())
		case rustplus.CameraEventUnsubscribed:
			fmt.Println("Unsubscribed from camera")
		case rustplus.CameraEventRender:
			frameCount++
			filename := fmt.Sprintf("camera_frame_%03d.png", frameCount)
			if err := rustplus.SaveImagePNG(event.Image, filename); err != nil {
				fmt.Printf("Error saving image: %v\n", err)
			} else {
				fmt.Printf("Frame %d saved to %s\n", frameCount, filename)
			}
		}
	})

	// Subscribe to camera
	if err := camera.Subscribe(playerId, token, cameraId); err != nil {
		fmt.Printf("Failed to subscribe: %v\n", err)
		return
	}

	// Wait for frames
	fmt.Println("\nWaiting for camera frames (30 seconds)...")
	time.Sleep(30 * time.Second)

	// Unsubscribe
	if err := camera.Unsubscribe(); err != nil {
		fmt.Printf("Failed to unsubscribe: %v\n", err)
	}

	fmt.Printf("\nCaptured %d frames\n", frameCount)
}

// Helper functions

func getServerInfo(client *rustplus.RustPlus, playerId string, token int32) {
	fmt.Println("\nGetting server info...")
	resp, err := client.GetInfo(playerId, token, true)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if resp.Info != nil {
		info := resp.Info
		fmt.Printf("Server: %s\n", info.Name)
		fmt.Printf("Map: %s (Size: %d, Seed: %d)\n", info.Map, info.MapSize, info.Seed)
		fmt.Printf("Players: %d/%d (Queued: %d)\n", info.Players, info.MaxPlayers, info.QueuedPlayers)
		fmt.Printf("URL: %s\n", info.Url)
	}
}

func getServerTime(client *rustplus.RustPlus, playerId string, token int32) {
	fmt.Println("\nGetting server time...")
	resp, err := client.GetTime(playerId, token, true)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if resp.Time != nil {
		t := resp.Time
		fmt.Printf("Time: %.2f\n", t.Time)
		fmt.Printf("Day length: %.2f minutes\n", t.DayLengthMinutes)
		fmt.Printf("Sunrise: %.2f, Sunset: %.2f\n", t.Sunrise, t.Sunset)
	}
}

func getTeamInfo(client *rustplus.RustPlus, playerId string, token int32) {
	fmt.Println("\nGetting team info...")
	resp, err := client.GetTeamInfo(playerId, token, true)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if resp.TeamInfo != nil {
		team := resp.TeamInfo
		fmt.Printf("Team members: %d\n", len(team.Members))
		fmt.Printf("Leader: %d\n", team.LeaderSteamId)
		for i, member := range team.Members {
			if i >= 5 {
				break
			}
			status := "Offline"
			if member.IsOnline {
				status = "Online"
			}
			alive := "Dead"
			if member.IsAlive {
				alive = "Alive"
			}
			fmt.Printf("- %s (%s, %s) at (%.1f, %.1f)\n",
				member.Name, status, alive, member.X, member.Y)
		}
	}
}

func getMapMarkers(client *rustplus.RustPlus, playerId string, token int32) {
	fmt.Println("\nGetting map markers...")
	resp, err := client.GetMapMarkers(playerId, token, true)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if resp.MapMarkers != nil {
		fmt.Printf("Map markers: %d\n", len(resp.MapMarkers.Markers))
		for i, marker := range resp.MapMarkers.Markers {
			if i >= 10 {
				break
			}
			fmt.Printf("[%d] Type: %d at (%.1f, %.1f)\n",
				marker.Id, marker.Type, marker.X, marker.Y)
			if marker.Name != nil {
				fmt.Printf("Name: %s\n", *marker.Name)
			}
		}
	}
}

func getEntityInfo(client *rustplus.RustPlus, playerId string, token int32, entityId uint32) {
	fmt.Printf("\nGetting entity %d info...\n", entityId)
	resp, err := client.GetEntityInfo(playerId, token, entityId, true)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if resp.EntityInfo != nil {
		entity := resp.EntityInfo
		fmt.Printf("Entity type: %d\n", entity.Type)
		if entity.Payload != nil {
			fmt.Printf("Value: %v\n", entity.Payload.Value)
			fmt.Printf("Capacity: %d\n", entity.Payload.Capacity)
			if len(entity.Payload.Items) > 0 {
				fmt.Printf("Items: %d\n", len(entity.Payload.Items))
				for i, item := range entity.Payload.Items {
					if i >= 5 {
						break
					}
					fmt.Printf("- Item ID: %d, Quantity: %d\n",
						item.ItemId, item.Quantity)
				}
			}
		}
	}
}

func handleBroadcast(broadcast *pb.AppBroadcast) {
	if broadcast.TeamMessage != nil && broadcast.TeamMessage.Message != nil {
		msg := broadcast.TeamMessage.Message
		fmt.Printf("Team message from %s: %s\n", msg.Name, msg.Message)
	}
	if broadcast.TeamChanged != nil {
		fmt.Println("Team changed")
	}
	if broadcast.EntityChanged != nil {
		fmt.Printf("Entity %d changed\n", broadcast.EntityChanged.GetEntityId())
	}
	if broadcast.ClanMessage != nil && broadcast.ClanMessage.Message != nil {
		msg := broadcast.ClanMessage.Message
		fmt.Printf("Clan message from %s: %s\n", msg.Name, msg.Message)
	}
}
