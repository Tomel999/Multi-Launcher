package main

import "multilauncherwails/launcher"

// GetSystemMemory exposes the total physical RAM (GB) to the frontend so the
// memory sliders can be sized against the real installed memory.
func (a *App) GetSystemMemory() int {
	return launcher.SystemMemoryGB()
}