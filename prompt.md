# AssemblyAI Desktop App Spec

## Framework
**Fyne** - Pure Go, cross-platform GUI

## Window Layout
```
┌─────────────────────────────────────┐
│ 🎙️ AssemblyAI Transcriber          │
├─────────────────────────────────────┤
│ API Key: [____________________] ⚙️  │
├─────────────────────────────────────┤
│ [🔴 Start] [⏹️ Stop] [🗑️ Clear] [📋 Copy] │
├─────────────────────────────────────┤
│ Status: Ready / Recording / Error   │
├─────────────────────────────────────┤
│ ┌─────────────────────────────────┐ │
│ │                                 │ │
│ │    Transcribed text appears     │ │
│ │         here in real-time       │ │
│ │                                 │ │
│ │                                 │ │
│ └─────────────────────────────────┘ │
└─────────────────────────────────────┘
```

## Core Components
- **Entry widget** - API key input
- **Button container** - 4 action buttons in row
- **Label** - Status indicator with colour coding
- **Scroll container** - Multi-line text display for transcripts
- **Menu bar** - Settings, About, Quit

## Key Features
- **Auto-save API key** to config file
- **Real-time text updates** as transcription streams
- **Audio permission handling** with user prompts
- **Error dialogs** for connection/auth failures

## Audio Integration
- **PortAudio** bindings for cross-platform microphone access
- **16kHz, mono** capture directly to AssemblyAI WebSocket
- **Visual feedback** during recording (pulsing button/status)

## Distribution
- Single executable per platform for linux
