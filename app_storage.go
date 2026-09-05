package main

import "multilauncherwails/launcher"

func (a *App) DiskUsage() []launcher.StorageCategory {
	return launcher.DiskUsage()
}

func (a *App) CleanCache() launcher.CleanReport {
	return launcher.CleanCache()
}

func (a *App) InstanceSize(name string) int64 {
	return launcher.InstanceSize(name)
}
