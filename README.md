# AssemblyAI Voice Transcriber

A real-time voice transcription application using AssemblyAI's streaming API.

## Features

- Real-time speech-to-text transcription
- Clean, formatted output with proper capitalization and punctuation
- Audio capture with configurable buffer sizes
- Persistent API key storage
- Copy transcribed text to clipboard
- Cross-platform GUI built with Fyne

## Requirements

- Go 1.21 or later
- AssemblyAI API key
- Audio input device (microphone)

## Installation

1. Clone this repository
2. Install dependencies:
   ```bash
   go mod tidy
   ```
3. Build the application:
   ```bash
   go build -o dict
   ```

## Usage

1. Run the application:
   ```bash
   ./dict
   ```
2. Enter your AssemblyAI API key in the password field
3. Click the save button (gear icon) to persist your API key
4. Click "Start" to begin recording and transcription
5. Click "Stop" to end the session
6. Use "Copy" to copy transcribed text to clipboard
7. Use "Clear" to clear the text area

## Configuration

The application saves your API key to `~/.assemblyai-transcriber.json` for future sessions.

## Dependencies

- [Fyne](https://fyne.io/) - Cross-platform GUI toolkit
- [Malgo](https://github.com/gen2brain/malgo) - Audio capture
- [Gorilla WebSocket](https://github.com/gorilla/websocket) - WebSocket client
- [AssemblyAI](https://www.assemblyai.com/) - Real-time speech recognition API