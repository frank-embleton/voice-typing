package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/gen2brain/malgo"
	"github.com/gorilla/websocket"
)

type App struct {
	fyneApp    fyne.App
	window     fyne.Window
	apiEntry   *widget.Entry
	startBtn   *widget.Button
	stopBtn    *widget.Button
	clearBtn   *widget.Button
	copyBtn    *widget.Button
	statusLbl  *widget.Label
	textArea   *widget.Entry
	
	// Audio and WebSocket
	ws         *websocket.Conn
	malgoCtx   *malgo.AllocatedContext
	device     *malgo.Device
	recording  bool
	
	// Transcript tracking
	finalText  string
	partialText string
	lastTurnOrder int
	lastTurnFinal string
	mu         sync.RWMutex
}

type AssemblyMessage struct {
	Type      string  `json:"type"`
	ID        string  `json:"id,omitempty"`
	ExpiresAt int64   `json:"expires_at,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	TurnIsFormatted bool `json:"turn_is_formatted,omitempty"`
	EndOfTurn bool `json:"end_of_turn,omitempty"`
	TurnOrder int `json:"turn_order,omitempty"`
	AudioDurationSeconds float64 `json:"audio_duration_seconds,omitempty"`
	SessionDurationSeconds float64 `json:"session_duration_seconds,omitempty"`
}

func main() {
	fyneApp := app.New()
	fyneApp.SetIcon(theme.MediaRecordIcon())
	
	myApp := &App{
		fyneApp: fyneApp,
	}
	
	myApp.setupUI()
	myApp.loadConfig()
	
	myApp.window.ShowAndRun()
}

func (a *App) setupUI() {
	a.window = a.fyneApp.NewWindow("🎙️ AssemblyAI Transcriber")
	a.window.Resize(fyne.NewSize(600, 500))
	
	// API Key entry
	a.apiEntry = widget.NewPasswordEntry()
	a.apiEntry.SetPlaceHolder("Enter your AssemblyAI API key")
	apiLabel := widget.NewLabel("API Key:")
	apiLabel.Resize(fyne.NewSize(60, apiLabel.MinSize().Height))
	saveBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		a.saveConfig()
	})
	saveBtn.Resize(fyne.NewSize(40, saveBtn.MinSize().Height))
	
	apiContainer := container.NewBorder(nil, nil, apiLabel, saveBtn, a.apiEntry)
	
	// Buttons
	a.startBtn = widget.NewButtonWithIcon("Start", theme.MediaPlayIcon(), a.startRecording)
	a.stopBtn = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), a.stopRecording)
	a.clearBtn = widget.NewButtonWithIcon("Clear", theme.DeleteIcon(), a.clearText)
	a.copyBtn = widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), a.copyText)
	
	a.stopBtn.Disable()
	
	buttonContainer := container.NewHBox(
		a.startBtn,
		a.stopBtn,
		a.clearBtn,
		a.copyBtn,
	)
	
	// Status
	a.statusLbl = widget.NewLabel("Status: Ready")
	
	// Text area
	a.textArea = widget.NewMultiLineEntry()
	a.textArea.SetPlaceHolder("Transcribed text will appear here...")
	a.textArea.Wrapping = fyne.TextWrapWord
	textScroll := container.NewScroll(a.textArea)
	textScroll.SetMinSize(fyne.NewSize(580, 300))
	
	// Layout
	content := container.NewVBox(
		apiContainer,
		buttonContainer,
		a.statusLbl,
		textScroll,
	)
	
	a.window.SetContent(content)
}

func (a *App) startRecording() {
	if a.apiEntry.Text == "" {
		dialog.ShowError(fmt.Errorf("Please enter your AssemblyAI API key"), a.window)
		return
	}
	
	a.mu.Lock()
	defer a.mu.Unlock()
	
	if a.recording {
		return
	}
	
	a.updateStatus("Connecting...")
	a.startBtn.Disable()
	
	go func() {
		err := a.connectWebSocket()
		if err != nil {
			a.updateStatus("Error: " + err.Error())
			a.startBtn.Enable()
			return
		}
		
		err = a.startAudio()
		if err != nil {
			a.updateStatus("Audio Error: " + err.Error())
			a.startBtn.Enable()
			a.closeWebSocket()
			return
		}
		
		a.recording = true
		fyne.Do(func() {
			a.stopBtn.Enable()
		})
		fyne.Do(func() {
			a.updateStatus("Recording...")
		})
	}()
}

func (a *App) stopRecording() {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	if !a.recording {
		return
	}
	
	a.recording = false
	a.stopBtn.Disable()
	a.updateStatus("Stopping...")
	
	go func() {
		a.stopAudio()
		a.closeWebSocket()
		fyne.Do(func() {
			a.startBtn.Enable()
		})
		fyne.Do(func() {
			a.updateStatus("Ready")
		})
	}()
}

func (a *App) clearText() {
	a.mu.Lock()
	a.finalText = ""
	a.partialText = ""
	a.lastTurnOrder = -1
	a.lastTurnFinal = ""
	a.mu.Unlock()
	a.textArea.SetText("")
}

func (a *App) copyText() {
	a.window.Clipboard().SetContent(a.textArea.Text)
}

func (a *App) updateStatus(status string) {
	a.statusLbl.SetText("Status: " + status)
}

func (a *App) connectWebSocket() error {
	params := url.Values{}
	params.Set("sample_rate", "16000")
	params.Set("format_turns", "true")
	params.Set("end_of_turn_confidence_threshold", "0.7")
	params.Set("min_end_of_turn_silence_when_confident", "160")
	params.Set("max_turn_silence", "2400")
	
	wsURL := "wss://streaming.assemblyai.com/v3/ws?" + params.Encode()
	
	headers := make(map[string][]string)
	headers["Authorization"] = []string{a.apiEntry.Text}
	
	var err error
	a.ws, _, err = websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		return fmt.Errorf("failed to connect to AssemblyAI: %v", err)
	}
	
	go a.handleWebSocketMessages()
	return nil
}

func (a *App) closeWebSocket() {
	if a.ws != nil {
		// Send termination message
		terminateMsg := map[string]string{"type": "Terminate"}
		a.ws.WriteJSON(terminateMsg)
		a.ws.Close()
		a.ws = nil
	}
}

func (a *App) handleWebSocketMessages() {
	for {
		var msg AssemblyMessage
		err := a.ws.ReadJSON(&msg)
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			break
		}
		
		switch msg.Type {
		case "Begin":
			log.Printf("Session began: ID=%s", msg.ID)
		case "Turn":
			if msg.EndOfTurn {
				a.mu.Lock()
				if msg.TurnOrder == a.lastTurnOrder {
					// Replace the last turn's text with formatted version
					if a.lastTurnFinal != "" && a.finalText != "" {
						// Remove the last turn
						if len(a.finalText) >= len(a.lastTurnFinal) {
							a.finalText = a.finalText[:len(a.finalText)-len(a.lastTurnFinal)]
							if a.finalText != "" && a.finalText[len(a.finalText)-1:] == "\n" {
								a.finalText = a.finalText[:len(a.finalText)-1]
							}
						}
					}
					if a.finalText != "" {
						a.finalText += "\n"
					}
					a.finalText += msg.Transcript
				} else {
					// New turn
					if a.finalText != "" {
						a.finalText += "\n"
					}
					a.finalText += msg.Transcript
					a.lastTurnOrder = msg.TurnOrder
				}
				a.lastTurnFinal = msg.Transcript
				a.partialText = ""
				displayText := a.finalText
				a.mu.Unlock()
				
				fyne.Do(func() {
					a.textArea.SetText(displayText)
				})
			} else {
				// Partial transcript - always update partial text (even if empty)
				a.mu.Lock()
				a.partialText = msg.Transcript
				displayText := a.finalText
				if a.partialText != "" {
					if displayText != "" {
						displayText += "\n" + a.partialText
					} else {
						displayText = a.partialText
					}
				}
				a.mu.Unlock()
				
				fyne.Do(func() {
					a.textArea.SetText(displayText)
				})
			}
		case "Termination":
			log.Printf("Session terminated")
		}
	}
}

func (a *App) startAudio() error {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		log.Printf("Malgo: %s", message)
	})
	if err != nil {
		return fmt.Errorf("failed to initialize audio context: %v", err)
	}
	a.malgoCtx = ctx
	
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = 16000
	deviceConfig.PeriodSizeInFrames = 800  // 50ms at 16kHz
	deviceConfig.Alsa.NoMMap = 1
	
	onSamples := func(pSample2, pSample []byte, framecount uint32) {
		// Send audio data to WebSocket
		if a.ws != nil && a.recording {
			a.ws.WriteMessage(websocket.BinaryMessage, pSample)
		}
	}
	
	device, err := malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onSamples,
	})
	if err != nil {
		ctx.Uninit()
		return fmt.Errorf("failed to initialize capture device: %v", err)
	}
	a.device = device
	
	err = device.Start()
	if err != nil {
		device.Uninit()
		ctx.Uninit()
		return fmt.Errorf("failed to start device: %v", err)
	}
	
	return nil
}

func (a *App) stopAudio() {
	if a.device != nil {
		a.device.Stop()
		a.device.Uninit()
		a.device = nil
	}
	
	if a.malgoCtx != nil {
		a.malgoCtx.Uninit()
		a.malgoCtx = nil
	}
}

func (a *App) getConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".assemblyai-transcriber.json")
}

func (a *App) loadConfig() {
	configPath := a.getConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	
	var config map[string]string
	if err := json.Unmarshal(data, &config); err != nil {
		return
	}
	
	if apiKey, exists := config["api_key"]; exists {
		a.apiEntry.SetText(apiKey)
	}
}

func (a *App) saveConfig() {
	config := map[string]string{
		"api_key": a.apiEntry.Text,
	}
	
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		dialog.ShowError(err, a.window)
		return
	}
	
	configPath := a.getConfigPath()
	err = os.WriteFile(configPath, data, 0600)
	if err != nil {
		dialog.ShowError(err, a.window)
		return
	}
	
	dialog.ShowInformation("Config Saved", "API key has been saved", a.window)
}