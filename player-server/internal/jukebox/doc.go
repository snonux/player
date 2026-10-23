// Package jukebox is the optional A02a server-audio engine: remote-control
// playback on the server host's speakers. Ordinary browser and Android
// streaming never depend on this package. When JUKEBOX_ENABLED is unset or
// false the engine is not wired and clients see no "This server's speakers"
// target. The Adapter interface keeps MPV (or a test fake) injectable and
// bounded: one process, confined media paths, and a hard IPC timeout.
package jukebox
