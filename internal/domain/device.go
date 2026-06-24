package domain

import (
	"time"

	
)


type DeviceStatus string

const(
	StatusOnline DeviceStatus = "online"
	StatusOffline DeviceStatus = "offline"
	StatusUnauthorized DeviceStatus = "unauthorized"
)

type DisplayInfo struct{
	Density int32
	Fps int32
	Height int32
	Rotation int32
	Size float64
	Width int32
	XDPI float64
	YDPI float64
}

type BatteryInfo struct{
	Level int
	IsCharging bool
	Health string
}

type NetworkInfo struct{
	Connected bool
	WiFiSSID string
	IP string
	MobileData bool
	Airplane bool
}

type DeviceInfo struct{
	Model string
	MarketName string
	Manufacturer string
	AndroidVersion string
	SDKVersion int
	CPUABI string
	RAMMB int64
	StorageMB int64
}

type DeviceState struct{
	Status DeviceStatus
	Battery BatteryInfo
	Network NetworkInfo
}
type Device struct{
	Serial string
	ProviderIP string

	Info DeviceInfo
	Display DisplayInfo
	State DeviceState

	ConnectedAt time.Time
	LastSeen time.Time
}
