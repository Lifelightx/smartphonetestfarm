package goios

import (
	"context"
	"fmt"
	"time"

	"protean-provider/internal/domain"
)

// IOSDeviceEntry represents a single device returned by `ios list`.
type IOSDeviceEntry struct {
	UDID           string `json:"udid"`
	DeviceName     string `json:"name"`
	ProductType    string `json:"type"`
	ProductVersion string `json:"version"`
}

// DeviceManager implements platform.DeviceManager for iOS.
type DeviceManager struct {
	client Client
}

// NewDeviceManager creates a new DeviceManager.
func NewDeviceManager(client Client) *DeviceManager {
	return &DeviceManager{client: client}
}

var productTypeMap = map[string]string{
	// iPhone 8 & 8 Plus
	"iPhone10,1": "iPhone 8",
	"iPhone10,4": "iPhone 8",
	"iPhone10,2": "iPhone 8 Plus",
	"iPhone10,5": "iPhone 8 Plus",

	// iPhone X
	"iPhone10,3": "iPhone X",
	"iPhone10,6": "iPhone X",

	// iPhone XS & XS Max
	"iPhone11,2": "iPhone XS",
	"iPhone11,4": "iPhone XS Max",
	"iPhone11,6": "iPhone XS Max",

	// iPhone XR
	"iPhone11,8": "iPhone XR",

	// iPhone 11 series
	"iPhone12,1": "iPhone 11",
	"iPhone12,3": "iPhone 11 Pro",
	"iPhone12,5": "iPhone 11 Pro Max",

	// iPhone SE (2nd Gen)
	"iPhone12,8": "iPhone SE (2nd Gen)",

	// iPhone 12 series
	"iPhone13,1": "iPhone 12 mini",
	"iPhone13,2": "iPhone 12",
	"iPhone13,3": "iPhone 12 Pro",
	"iPhone13,4": "iPhone 12 Pro Max",

	// iPhone 13 series
	"iPhone14,4": "iPhone 13 mini",
	"iPhone14,5": "iPhone 13",
	"iPhone14,2": "iPhone 13 Pro",
	"iPhone14,3": "iPhone 13 Pro Max",

	// iPhone SE (3rd Gen)
	"iPhone14,6": "iPhone SE (3rd Gen)",

	// iPhone 14 series
	"iPhone14,7": "iPhone 14",
	"iPhone14,8": "iPhone 14 Plus",
	"iPhone15,2": "iPhone 14 Pro",
	"iPhone15,3": "iPhone 14 Pro Max",

	// iPhone 15 series
	"iPhone15,4": "iPhone 15",
	"iPhone15,5": "iPhone 15 Plus",
	"iPhone16,1": "iPhone 15 Pro",
	"iPhone16,2": "iPhone 15 Pro Max",

	// iPhone 16 series
	"iPhone17,1": "iPhone 16",
	"iPhone17,2": "iPhone 16 Plus",
	"iPhone17,3": "iPhone 16 Pro",
	"iPhone17,4": "iPhone 16 Pro Max",

	// iPhone 17 series (predicted/upcoming mappings)
	"iPhone18,1": "iPhone 17",
	"iPhone18,2": "iPhone 17 Plus",
	"iPhone18,3": "iPhone 17 Pro",
	"iPhone18,4": "iPhone 17 Pro Max",
}

var productTypeRAMMap = map[string]int64{
	// iPhone 8 / 8 Plus
	"iPhone10,1": 2048,
	"iPhone10,4": 2048,
	"iPhone10,2": 3072,
	"iPhone10,5": 3072,
	// iPhone X
	"iPhone10,3": 3072,
	"iPhone10,6": 3072,
	// iPhone XS / XS Max / XR
	"iPhone11,2": 4096,
	"iPhone11,4": 4096,
	"iPhone11,6": 4096,
	"iPhone11,8": 3072,
	// iPhone 11 series
	"iPhone12,1": 4096,
	"iPhone12,3": 4096,
	"iPhone12,5": 4096,
	// iPhone SE (2nd Gen)
	"iPhone12,8": 3072,
	// iPhone 12 series
	"iPhone13,1": 4096,
	"iPhone13,2": 4096,
	"iPhone13,3": 6144,
	"iPhone13,4": 6144,
	// iPhone 13 series
	"iPhone14,4": 4096,
	"iPhone14,5": 4096,
	"iPhone14,2": 6144,
	"iPhone14,3": 6144,
	// iPhone SE (3rd Gen)
	"iPhone14,6": 4096,
	// iPhone 14 series
	"iPhone14,7": 6144,
	"iPhone14,8": 6144,
	"iPhone15,2": 6144,
	"iPhone15,3": 6144,
	// iPhone 15 series
	"iPhone15,4": 6144,
	"iPhone15,5": 6144,
	"iPhone16,1": 8192,
	"iPhone16,2": 8192,
	// iPhone 16 series
	"iPhone17,1": 8192,
	"iPhone17,2": 8192,
	"iPhone17,3": 8192,
	"iPhone17,4": 8192,
}

func resolveRAM(productType string) int64 {
	if ram, ok := productTypeRAMMap[productType]; ok {
		return ram
	}
	return 0
}

func resolveModelName(productType string) string {
	if name, ok := productTypeMap[productType]; ok {
		return name
	}
	return productType
}

// Discover lists all connected iOS devices.
func (dm *DeviceManager) Discover(ctx context.Context) ([]*domain.Device, error) {
	out, err := dm.client.RunNoUDID(ctx, "list")
	if err != nil {
		return nil, fmt.Errorf("goios discover: %w", err)
	}

	entries, err := parseIOSList(string(out))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ios list array: %w", err)
	}

	var devices []*domain.Device
	for _, entry := range entries {
		now := time.Now()
		devices = append(devices, &domain.Device{
			Serial:     entry.UDID,
			Platform:   "ios",
			ProviderIP: "127.0.0.1",
			Info: domain.DeviceInfo{
				Model:          resolveModelName(entry.ProductType),
				Manufacturer:   "Apple",
				AndroidVersion: entry.ProductVersion, // iOS Version mapped to AndroidVersion field for compatibility
				RAMMB:          resolveRAM(entry.ProductType),
			},
			ConnectedAt: now,
			LastSeen:    now,
		})
	}

	return devices, nil
}

// GetProperties retrieves detailed lockdown and hardware configuration for an iOS device.
func (dm *DeviceManager) GetProperties(ctx context.Context, serial string) (*domain.Device, error) {
	out, err := dm.client.Run(ctx, serial, "info")
	if err != nil {
		return nil, fmt.Errorf("goios get info for %s: %w", serial, err)
	}

	infoMap, _ := parseJSONMap(string(out))

	// Fetch display configurations
	displayOut, err := dm.client.Run(ctx, serial, "info", "display")
	var disp domain.DisplayInfo
	if err == nil {
		if dispMap, err := parseJSONMap(string(displayOut)); err == nil {
			if w, ok := dispMap["width"].(float64); ok {
				disp.Width = int32(w)
			}
			if h, ok := dispMap["height"].(float64); ok {
				disp.Height = int32(h)
			}
		}
	}

	// Default fallback display details
	if disp.Width == 0 {
		disp.Width = 1170
		disp.Height = 2532
		disp.Fps = 60
	}

	// Fetch storage configuration
	diskOut, err := dm.client.Run(ctx, serial, "diskspace")
	var storageMB int64
	if err == nil {
		if diskMap, err := parseJSONMap(string(diskOut)); err == nil {
			if tb, ok := diskMap["TotalBytes"].(float64); ok {
				storageMB = int64(tb) / 1024 / 1024
			}
		}
	}

	// Fetch battery configurations
	batteryOut, err := dm.client.Run(ctx, serial, "batterycheck")
	var batteryLevel int
	if err == nil {
		if batMap, err := parseJSONMap(string(batteryOut)); err == nil {
			if cap, ok := batMap["BatteryCurrentCapacity"].(float64); ok {
				batteryLevel = int(cap)
			}
		}
	}

	productType := getString(infoMap, "ProductType")

	now := time.Now()
	dev := &domain.Device{
		Serial:     serial,
		Platform:   "ios",
		ProviderIP: "127.0.0.1",
		Display:    disp,
		Info: domain.DeviceInfo{
			Model:          resolveModelName(productType),
			Manufacturer:   "Apple",
			AndroidVersion: getString(infoMap, "ProductVersion"),
			RAMMB:          resolveRAM(productType),
			StorageMB:      storageMB,
		},
		State: domain.DeviceState{
			Battery: domain.BatteryInfo{
				Level: batteryLevel,
			},
		},
		ConnectedAt: now,
		LastSeen:    now,
	}

	return dev, nil
}

func getString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
