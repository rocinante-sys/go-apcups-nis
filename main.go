package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"red-mamba.com/go-apcups-nis/constants"
)

// GlobalSafeMap provides concurrent-safe access to a map.
var GlobalValuesMap = struct {
	mu sync.RWMutex
	m  map[string]string
}{m: make(map[string]string)}

type ConfigUPS struct {
	ApcUPS      string `json:"apcups"`
	MqttAddress string `json:"mqtt_address"`
	Divider     int32  `json:"divider"`
	Decimals    int32  `json:"decimals"`
	Unit        string `json:"unit"`
}

type UPSData struct {
	Fields []ConfigUPS
}

// Build apcaccess-like lines (KEY : value). We'll stream them as framed lines for apcaccess clients.
func (u UPSData) StatusLines(serverHost string) []string {
	now := time.Now()
	start := now.Add(-24 * time.Hour)

	lines := []string{
		"APC      : 001,036,0891\n",
		fmt.Sprintf("DATE     : %s\n", now.Format("2006-01-02 15:04:05 -0700")),
		fmt.Sprintf("HOSTNAME : %s\n", serverHost),
		fmt.Sprintf("VERSION  : %s\n", constants.ApcVersion),
		fmt.Sprintf("UPSNAME  : %s\n", constants.ApcUpsName),
		fmt.Sprintf("CABLE    : %s\n", constants.ApcCable),
		fmt.Sprintf("DRIVER   : %s\n", constants.ApcDriver),
		fmt.Sprintf("MODEL    : %s\n", constants.ApcModel),
		fmt.Sprintf("UPSMODE  : %s\n", constants.ApcUpsMode),
		fmt.Sprintf("STARTTIME: %s\n", start.Format("2006-01-02 15:04:05 -0700")),
		fmt.Sprintf("NOMPOWER : %s Watts\n", constants.ApcNomPow),
	}

	GlobalValuesMap.mu.RLock()
	for _, F := range u.Fields {
		lines = append(lines, fmt.Sprintf("%s : %s %s\n", F.ApcUPS, GlobalValuesMap.m[F.ApcUPS], F.Unit))
	}
	GlobalValuesMap.mu.RUnlock()

	lines = append(
		lines,
		[]string{
			fmt.Sprintf("SERIALNO : %s\n", constants.ApcSerialNo),
			fmt.Sprintf("FIRMWARE : %s\n", constants.ApcFirmware),
			fmt.Sprintf("END APC  : %s\n", now.Format("2006-01-02 15:04:05 -0700")),
		}...,
	)

	return lines
}

// --- Framing helpers (apcaccess / NIS style) ---

// Try to read a framed command: [2 bytes length][payload bytes].
// Returns (cmd, true, nil) if framed, ("", false, nil) if not framed.
func tryReadFramedCommand(r *bufio.Reader) (string, bool, error) {
	// Peek 2 bytes without consuming.
	hdr, err := r.Peek(2)
	if err != nil {
		return "", false, err
	}

	n := int(binary.BigEndian.Uint16(hdr))

	// Heuristics: framed commands are short ASCII words ("status", "events", etc.).
	// If n is 0 or absurdly large, assume not framed.
	if n <= 0 || n > 4096 {
		return "", false, nil
	}

	// We expect the stream to have 2+n bytes available; if not, let Read block normally.
	// Consume header.
	if _, err := r.Discard(2); err != nil {
		return "", false, err
	}

	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return "", true, err
	}

	return strings.TrimSpace(string(payload)), true, nil
}

// Write one framed message: [2 bytes length][payload].
func writeFrame(w *bufio.Writer, s string) error {
	b := []byte(s)
	if len(b) > 65535 {
		return fmt.Errorf("frame too large: %d", len(b))
	}
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(b) > 0 {
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	return w.Flush()
}

// Write framed "status" response: one line per frame, then a zero-length frame terminator.
func writeFramedStatus(w *bufio.Writer, lines []string) error {
	for _, line := range lines {
		if err := writeFrame(w, line); err != nil {
			return err
		}
	}
	// terminator
	return writeFrame(w, "")
}

// --- Plain-text fallback (for manual netcat testing) ---

func writePlainStatus(w *bufio.Writer, lines []string) error {
	for _, line := range lines {
		if _, err := w.WriteString(line + "\n"); err != nil {
			return err
		}
	}
	return w.Flush()
}

func handleConn(conn net.Conn, data *atomic.Value, serverHost string) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	for {
		// First, try framed (apcaccess).
		cmd, framed, err := tryReadFramedCommand(r)
		if err != nil {
			return
		}

		if !framed {
			// Fallback: plain-text line protocol (status\n)
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd = strings.TrimSpace(line)
		}

		cmdLower := strings.ToLower(cmd)

		switch {
		case cmdLower == "":
			continue

		case cmdLower == "status" || strings.HasPrefix(cmdLower, "status "):
			u := data.Load().(UPSData)
			lines := u.StatusLines(serverHost)

			if framed {
				// apcaccess expects framed reply with zero-length terminator
				_ = writeFramedStatus(w, lines)
				return // many clients do one request per connection
			}

			_ = writePlainStatus(w, lines)
			return

		case cmdLower == "version":
			if framed {
				_ = writeFrame(w, "3.14.14 (go-nis-emulator)")
				_ = writeFrame(w, "")
			} else {
				_, _ = w.WriteString("3.14.14 (go-nis-emulator)\n")
				_ = w.Flush()
			}
			return

		case cmdLower == "help" || cmdLower == "?":
			msg := "Commands: status, events, list, version, help, quit"
			if framed {
				_ = writeFrame(w, msg)
				_ = writeFrame(w, "")
			} else {
				_, _ = w.WriteString(msg + "\n")
				_ = w.Flush()
			}
			return

		case cmdLower == "list":
			if framed {
				_ = writeFrame(w, "UPS: 1")
				_ = writeFrame(w, "1: "+constants.ApcUpsName)
				_ = writeFrame(w, "")
			} else {
				_, _ = w.WriteString("UPS: 1\n")
				_, _ = w.WriteString("1: " + constants.ApcUpsName + "\n")
				_ = w.Flush()
			}
			return

		case cmdLower == "events":
			if framed {
				_ = writeFrame(w, "No events.")
				_ = writeFrame(w, "")
			} else {
				_, _ = w.WriteString("No events.\n")
				_ = w.Flush()
			}
			return

		case cmdLower == "quit" || cmdLower == "exit":
			// Just close
			return

		default:
			errMsg := "ERR Unknown command: " + cmd
			if framed {
				_ = writeFrame(w, errMsg)
				_ = writeFrame(w, "")
			} else {
				_, _ = w.WriteString(errMsg + "\n")
				_ = w.Flush()
			}
			return
		}
	}
}

func main() {
	// 1. Read the JSON file content
	fileBytes, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	// 2. Unmarshal the data into the Config struct
	var UpsValues []ConfigUPS
	err = json.Unmarshal(fileBytes, &UpsValues)
	if err != nil {
		log.Fatalf("Error unmarshaling JSON: %v", err)
	}

	// Mock data (replace with real inputs later)
	data := atomic.Value{}
	data.Store(UPSData{
		Fields: UpsValues,
	})

	var f mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
		u := data.Load().(UPSData)
		for _, F := range u.Fields {
			if F.MqttAddress == msg.Topic() {
				if constants.ApcDebug == "true" {
					fmt.Printf("TOPIC: %s >> %s >> %s\n", msg.Topic(), F.ApcUPS, msg.Payload())
				}
				if F.Divider == 0 {
					GlobalValuesMap.mu.Lock()
					GlobalValuesMap.m[F.ApcUPS] = string(msg.Payload())
					GlobalValuesMap.mu.Unlock()
					return
				}
				f, _ := strconv.ParseFloat(string(msg.Payload()), 64)
				f = f / float64(F.Divider)
				GlobalValuesMap.mu.Lock()
				GlobalValuesMap.m[F.ApcUPS] = fmt.Sprintf("%.*f", F.Decimals, f)
				GlobalValuesMap.mu.Unlock()
				return
			}
		}
	}

	// mqtt.DEBUG = log.New(os.Stdout, "", 0)
	mqtt.ERROR = log.New(os.Stdout, "", 0)
	opts := mqtt.NewClientOptions().AddBroker(constants.MqttServer).SetClientID(constants.MqttDevice)
	opts.SetKeepAlive(2 * time.Second)
	opts.SetDefaultPublishHandler(f)
	opts.SetPingTimeout(1 * time.Second)
	opts.SetConnectionNotificationHandler(func(client mqtt.Client, notification mqtt.ConnectionNotification) {
		switch n := notification.(type) {
		case mqtt.ConnectionNotificationConnected:
			fmt.Printf("[NOTIFICATION] connected\n")
		case mqtt.ConnectionNotificationConnecting:
			fmt.Printf("[NOTIFICATION] connecting (isReconnect=%t) [%d]\n", n.IsReconnect, n.Attempt)
		case mqtt.ConnectionNotificationFailed:
			fmt.Printf("[NOTIFICATION] connection failed: %v\n", n.Reason)
		case mqtt.ConnectionNotificationLost:
			fmt.Printf("[NOTIFICATION] connection lost: %v\n", n.Reason)
		case mqtt.ConnectionNotificationBroker:
			fmt.Printf("[NOTIFICATION] broker connection: %s\n", n.Broker.String())
		case mqtt.ConnectionNotificationBrokerFailed:
			fmt.Printf("[NOTIFICATION] broker connection failed: %v [%s]\n", n.Reason, n.Broker.String())
		}
	})
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		for _, C := range UpsValues {
			if token := client.Subscribe(C.MqttAddress, 0, nil); token.Wait() && token.Error() != nil {
				fmt.Println(token.Error())
				os.Exit(1)
			}
		}
	})

	c := mqtt.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	// for i := 0; i < 5; i++ {
	// 	text := fmt.Sprintf("this is msg #%d!", i)
	// 	token := c.Publish("go-mqtt/sample", 0, false, text)
	// 	token.Wait()
	// }

	addr := ":3551"

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
	log.Printf("go-apcups-nis listening on %s", addr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	serverHost, _ := os.Hostname()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				log.Println("shutting down")
				return
			default:
			}
			log.Printf("accept: %v", err)
			continue
		}
		go handleConn(conn, &data, serverHost)
	}
}
