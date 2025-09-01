package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/gen2brain/malgo"
	"github.com/gorilla/websocket"
)

type App struct {
	fyneApp    fyne.App
	window     fyne.Window
	startBtn   *widget.Button
	stopBtn    *widget.Button
	clearBtn   *widget.Button
	copyBtn    *widget.Button
	processBtn *widget.Button
	undoBtn    *widget.Button
	settingsBtn *widget.Button
	statusLbl  *widget.Label
	textArea   *widget.Entry
	
	// Audio and WebSocket
	ws         *websocket.Conn
	malgoCtx   *malgo.AllocatedContext
	device     *malgo.Device
	recording  bool
	
	// API Configuration
	assemblyAPIKey string
	groqAPIKey     string
	groqModel      string
	groqEndpoint   string
	systemPrompt   string
	
	// Transcript tracking
	finalText  string
	partialText string
	lastTurnOrder int
	lastTurnFinal string
	
	// Undo functionality
	previousText string
	
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

type GroqRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type GroqResponse struct {
	Choices []Choice `json:"choices"`
	Error   *GroqError `json:"error,omitempty"`
}

type Choice struct {
	Message Message `json:"message"`
}

type GroqError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
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
	
	// Header with settings
	a.settingsBtn = widget.NewButtonWithIcon("Settings", theme.SettingsIcon(), a.showSettingsModal)
	headerContainer := container.NewBorder(nil, nil, nil, a.settingsBtn, widget.NewLabel("🎙️ AssemblyAI Transcriber"))
	
	// Buttons
	a.startBtn = widget.NewButtonWithIcon("Start", theme.MediaPlayIcon(), a.startRecording)
	a.stopBtn = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), a.stopRecording)
	a.clearBtn = widget.NewButtonWithIcon("Clear", theme.DeleteIcon(), a.clearText)
	a.copyBtn = widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), a.copyText)
	a.processBtn = widget.NewButtonWithIcon("Process with LLM", theme.ComputerIcon(), a.processWithLLM)
	
	a.stopBtn.Disable()
	
	buttonContainer := container.NewHBox(
		a.startBtn,
		a.stopBtn,
		a.clearBtn,
		a.copyBtn,
		a.processBtn,
	)
	
	// Status
	a.statusLbl = widget.NewLabel("Status: Ready")
	
	// Text area (make it editable)
	a.textArea = widget.NewMultiLineEntry()
	a.textArea.SetPlaceHolder("Transcribed text will appear here... (editable)")
	a.textArea.Wrapping = fyne.TextWrapWord
	textScroll := container.NewScroll(a.textArea)
	textScroll.SetMinSize(fyne.NewSize(580, 300))
	
	// Layout
	content := container.NewVBox(
		headerContainer,
		buttonContainer,
		a.statusLbl,
		textScroll,
	)
	
	a.window.SetContent(content)
	a.setupKeyboardShortcuts()
}

func (a *App) setupKeyboardShortcuts() {
	// Keyboard shortcuts
	space := &desktop.CustomShortcut{KeyName: fyne.KeySpace, Modifier: 0}
	ctrlR := &desktop.CustomShortcut{KeyName: fyne.KeyR, Modifier: desktop.ControlModifier}
	ctrlS := &desktop.CustomShortcut{KeyName: fyne.KeyS, Modifier: desktop.ControlModifier}
	ctrlL := &desktop.CustomShortcut{KeyName: fyne.KeyL, Modifier: desktop.ControlModifier}
	ctrlC := &desktop.CustomShortcut{KeyName: fyne.KeyC, Modifier: desktop.ControlModifier}
	ctrlP := &desktop.CustomShortcut{KeyName: fyne.KeyP, Modifier: desktop.ControlModifier}
	
	// Start recording - Spacebar or Ctrl+R
	a.window.Canvas().AddShortcut(space, func(_ fyne.Shortcut) {
		if !a.recording {
			a.startRecording()
		}
	})
	a.window.Canvas().AddShortcut(ctrlR, func(_ fyne.Shortcut) {
		if !a.recording {
			a.startRecording()
		}
	})
	
	// Stop recording - Ctrl+S
	a.window.Canvas().AddShortcut(ctrlS, func(_ fyne.Shortcut) {
		if a.recording {
			a.stopRecording()
		}
	})
	
	// Clear - Ctrl+L
	a.window.Canvas().AddShortcut(ctrlL, func(_ fyne.Shortcut) {
		a.clearText()
	})
	
	// Copy - Ctrl+C
	a.window.Canvas().AddShortcut(ctrlC, func(_ fyne.Shortcut) {
		a.copyText()
	})
	
	// Process with LLM - Ctrl+P
	a.window.Canvas().AddShortcut(ctrlP, func(_ fyne.Shortcut) {
		a.processWithLLM()
	})
}

func (a *App) startRecording() {
	log.Printf("DEBUG: Start recording requested")
	if a.assemblyAPIKey == "" {
		log.Printf("DEBUG: No AssemblyAI API key configured")
		dialog.ShowError(fmt.Errorf("Please configure your AssemblyAI API key in Settings"), a.window)
		return
	}
	
	a.mu.Lock()
	defer a.mu.Unlock()
	
	if a.recording {
		log.Printf("DEBUG: Already recording, ignoring request")
		return
	}
	
	log.Printf("DEBUG: Starting recording process")
	a.updateStatus("Connecting...")
	a.startBtn.Disable()
	
	go func() {
		log.Printf("DEBUG: Attempting WebSocket connection")
		err := a.connectWebSocket()
		if err != nil {
			log.Printf("DEBUG: WebSocket connection failed: %v", err)
			a.updateStatus("Error: " + err.Error())
			a.startBtn.Enable()
			return
		}
		
		log.Printf("DEBUG: Attempting to start audio capture")
		err = a.startAudio()
		if err != nil {
			log.Printf("DEBUG: Audio capture failed: %v", err)
			a.updateStatus("Audio Error: " + err.Error())
			a.startBtn.Enable()
			a.closeWebSocket()
			return
		}
		
		log.Printf("DEBUG: Recording started successfully")
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

func (a *App) showSettingsModal() {
	// Create form fields
	assemblyAPIEntry := widget.NewPasswordEntry()
	assemblyAPIEntry.SetPlaceHolder("Enter AssemblyAI API key")
	assemblyAPIEntry.SetText(a.assemblyAPIKey)
	
	groqAPIEntry := widget.NewPasswordEntry()
	groqAPIEntry.SetPlaceHolder("Enter Groq API key")
	groqAPIEntry.SetText(a.groqAPIKey)
	
	modelEntry := widget.NewEntry()
	modelEntry.SetPlaceHolder("e.g., meta-llama/llama-4-maverick-17b-128e-instruct")
	if a.groqModel == "" {
		modelEntry.SetText("meta-llama/llama-4-maverick-17b-128e-instruct")
	} else {
		modelEntry.SetText(a.groqModel)
	}
	
	endpointEntry := widget.NewEntry()
	endpointEntry.SetPlaceHolder("API endpoint URL")
	if a.groqEndpoint == "" {
		endpointEntry.SetText("https://api.groq.com/openai/v1/chat/completions")
	} else {
		endpointEntry.SetText(a.groqEndpoint)
	}
	
	systemPromptEntry := widget.NewMultiLineEntry()
	systemPromptEntry.SetPlaceHolder("Enter system prompt for LLM processing...")
	systemPromptEntry.SetText(a.systemPrompt)
	systemPromptEntry.Resize(fyne.NewSize(400, 100))
	
	// Create form
	form := container.NewVBox(
		widget.NewLabel("AssemblyAI Settings"),
		widget.NewLabel("API Key:"),
		assemblyAPIEntry,
		
		widget.NewSeparator(),
		
		widget.NewLabel("Groq LLM Settings"),
		widget.NewLabel("API Key:"),
		groqAPIEntry,
		widget.NewLabel("Model:"),
		modelEntry,
		widget.NewLabel("Endpoint:"),
		endpointEntry,
		widget.NewLabel("System Prompt:"),
		systemPromptEntry,
	)
	
	// Save button
	saveBtn := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		a.assemblyAPIKey = assemblyAPIEntry.Text
		a.groqAPIKey = groqAPIEntry.Text
		a.groqModel = modelEntry.Text
		a.groqEndpoint = endpointEntry.Text
		a.systemPrompt = systemPromptEntry.Text
		
		a.saveConfig()
	})
	
	formWithSave := container.NewVBox(form, saveBtn)
	
	// Create modal dialog
	settingsDialog := dialog.NewCustom("Settings", "Close", formWithSave, a.window)
	settingsDialog.Resize(fyne.NewSize(500, 600))
	settingsDialog.Show()
}

func (a *App) processWithLLM() {
	if a.groqAPIKey == "" {
		dialog.ShowError(fmt.Errorf("Please configure Groq API key in Settings"), a.window)
		return
	}
	
	if a.systemPrompt == "" {
		dialog.ShowError(fmt.Errorf("Please configure system prompt in Settings"), a.window)
		return
	}
	
	text := a.textArea.Text
	if text == "" {
		a.updateStatus("No text to process")
		return
	}
	
	a.updateStatus("Processing with LLM...")
	a.processBtn.Disable()
	
	go func() {
		processedText, err := a.callGroqAPI(text)
		
		fyne.Do(func() {
			a.processBtn.Enable()
			if err != nil {
				a.updateStatus("LLM processing failed: " + err.Error())
				dialog.ShowError(err, a.window)
			} else {
				a.textArea.SetText(processedText)
				a.updateStatus("Text processed successfully")
			}
		})
	}()
}

func (a *App) callGroqAPI(text string) (string, error) {
	request := GroqRequest{
		Model: a.groqModel,
		Messages: []Message{
			{Role: "system", Content: a.systemPrompt},
			{Role: "user", Content: text},
		},
	}
	
	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %v", err)
	}
	
	req, err := http.NewRequest("POST", a.groqEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.groqAPIKey)
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call Groq API: %v", err)
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}
	
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Groq API error (status %d): %s", resp.StatusCode, string(body))
	}
	
	var response GroqResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %v", err)
	}
	
	if response.Error != nil {
		return "", fmt.Errorf("Groq API error: %s", response.Error.Message)
	}
	
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no response from Groq API")
	}
	
	return response.Choices[0].Message.Content, nil
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
	
	log.Printf("DEBUG: Connecting to AssemblyAI WebSocket: %s", wsURL)
	log.Printf("DEBUG: Using API key (first 10 chars): %s...", a.assemblyAPIKey[:min(10, len(a.assemblyAPIKey))])
	
	headers := make(map[string][]string)
	headers["Authorization"] = []string{a.assemblyAPIKey}
	
	var err error
	a.ws, _, err = websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		log.Printf("DEBUG: WebSocket connection failed: %v", err)
		return fmt.Errorf("failed to connect to AssemblyAI: %v", err)
	}
	
	log.Printf("DEBUG: WebSocket connected successfully")
	go a.handleWebSocketMessages()
	return nil
}

func (a *App) closeWebSocket() {
	if a.ws != nil {
		log.Printf("DEBUG: Closing WebSocket connection")
		// Send termination message
		terminateMsg := map[string]string{"type": "Terminate"}
		a.ws.WriteJSON(terminateMsg)
		a.ws.Close()
		a.ws = nil
		log.Printf("DEBUG: WebSocket closed")
	}
}

func (a *App) handleWebSocketMessages() {
	log.Printf("DEBUG: Starting WebSocket message handler")
	for {
		var msg AssemblyMessage
		err := a.ws.ReadJSON(&msg)
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				log.Printf("DEBUG: WebSocket read error: %v", err)
			}
			break
		}
		
		log.Printf("DEBUG: Received message type: %s", msg.Type)
		
		switch msg.Type {
		case "Begin":
			log.Printf("DEBUG: Session began: ID=%s", msg.ID)
		case "Turn":
			log.Printf("DEBUG: Turn message - EndOfTurn: %v, TurnOrder: %d, Transcript: '%s'", msg.EndOfTurn, msg.TurnOrder, msg.Transcript)
			if msg.EndOfTurn {
				a.mu.Lock()
				if msg.TurnOrder == a.lastTurnOrder {
					// Replace the last turn's text with formatted version
					log.Printf("DEBUG: Replacing existing turn %d", msg.TurnOrder)
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
					log.Printf("DEBUG: New turn %d", msg.TurnOrder)
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
				
				log.Printf("DEBUG: Final text updated to: '%s'", displayText)
				fyne.Do(func() {
					a.textArea.SetText(displayText)
				})
			} else {
				// Partial transcript - always update partial text (even if empty)
				log.Printf("DEBUG: Partial transcript: '%s'", msg.Transcript)
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
			log.Printf("DEBUG: Session terminated")
		default:
			log.Printf("DEBUG: Unknown message type: %s", msg.Type)
		}
	}
	log.Printf("DEBUG: WebSocket message handler exited")
}

func (a *App) startAudio() error {
	log.Printf("DEBUG: Initializing audio context")
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		log.Printf("DEBUG: Malgo: %s", message)
	})
	if err != nil {
		return fmt.Errorf("failed to initialize audio context: %v", err)
	}
	a.malgoCtx = ctx
	log.Printf("DEBUG: Audio context initialized successfully")
	
	log.Printf("DEBUG: Setting up audio device config")
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = 16000
	deviceConfig.PeriodSizeInFrames = 800  // 50ms at 16kHz
	deviceConfig.Alsa.NoMMap = 1
	log.Printf("DEBUG: Audio device config: Sample Rate=%d, Channels=%d, Format=%d", 
		deviceConfig.SampleRate, deviceConfig.Capture.Channels, deviceConfig.Capture.Format)
	
	var sampleCounter int
	onSamples := func(pSample2, pSample []byte, framecount uint32) {
		// Send audio data to WebSocket
		if a.ws != nil && a.recording {
			err := a.ws.WriteMessage(websocket.BinaryMessage, pSample)
			if err != nil {
				log.Printf("DEBUG: Failed to send audio data: %v", err)
			} else {
				// Only log every 100th sample to avoid spam
				sampleCounter++
				if sampleCounter%100 == 0 {
					log.Printf("DEBUG: Sent audio sample %d, size: %d bytes", sampleCounter, len(pSample))
				}
			}
		}
	}
	
	log.Printf("DEBUG: Initializing audio capture device")
	device, err := malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onSamples,
	})
	if err != nil {
		log.Printf("DEBUG: Failed to initialize audio device: %v", err)
		ctx.Uninit()
		return fmt.Errorf("failed to initialize capture device: %v", err)
	}
	a.device = device
	log.Printf("DEBUG: Audio device initialized successfully")
	
	log.Printf("DEBUG: Starting audio device")
	err = device.Start()
	if err != nil {
		log.Printf("DEBUG: Failed to start audio device: %v", err)
		device.Uninit()
		ctx.Uninit()
		return fmt.Errorf("failed to start device: %v", err)
	}
	
	log.Printf("DEBUG: Audio capture started successfully")
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
		// Set defaults
		a.groqModel = "meta-llama/llama-4-maverick-17b-128e-instruct"
		a.groqEndpoint = "https://api.groq.com/openai/v1/chat/completions"
		return
	}
	
	var config map[string]string
	if err := json.Unmarshal(data, &config); err != nil {
		return
	}
	
	if apiKey, exists := config["assembly_api_key"]; exists {
		a.assemblyAPIKey = apiKey
	}
	if groqKey, exists := config["groq_api_key"]; exists {
		a.groqAPIKey = groqKey
	}
	if model, exists := config["groq_model"]; exists {
		a.groqModel = model
	} else {
		a.groqModel = "meta-llama/llama-4-maverick-17b-128e-instruct"
	}
	if endpoint, exists := config["groq_endpoint"]; exists {
		a.groqEndpoint = endpoint
	} else {
		a.groqEndpoint = "https://api.groq.com/openai/v1/chat/completions"
	}
	if prompt, exists := config["system_prompt"]; exists {
		a.systemPrompt = prompt
	}
}

func (a *App) saveConfig() {
	config := map[string]string{
		"assembly_api_key": a.assemblyAPIKey,
		"groq_api_key":     a.groqAPIKey,
		"groq_model":       a.groqModel,
		"groq_endpoint":    a.groqEndpoint,
		"system_prompt":    a.systemPrompt,
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
	
	dialog.ShowInformation("Config Saved", "Settings have been saved", a.window)
}