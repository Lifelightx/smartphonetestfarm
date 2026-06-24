package adb

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"protean-provider/internal/domain"
)

// propertyMap defines the ADB getprop keys we care about and where they map
// in the domain.DeviceInfo struct.
var knownProps = []string{
	"ro.product.model",
	"ro.product.manufacturer",
	"ro.product.name",
	"ro.build.version.release",
	"ro.build.version.sdk",
	"ro.product.cpu.abi",
}

// FetchProperties retrieves all device properties from the device via ADB shell
// and returns a fully populated domain.Device. ctx should carry the property timeout.
func FetchProperties(ctx context.Context, c Client, serial string) (*domain.Device, error) {
	// ── 1. Basic identity via getprop ──────────────────────────────────────────
	info, err := fetchDeviceInfo(ctx, c, serial)
	if err != nil {
		return nil, fmt.Errorf("properties: device info: %w", err)
	}

	// ── 2. Display geometry via wm size / wm density ──────────────────────────
	display, err := fetchDisplayInfo(ctx, c, serial)
	if err != nil {
		// Non-fatal: some minimal devices don't support wm.
		display = domain.DisplayInfo{}
	}

	// ── 3. Battery ────────────────────────────────────────────────────────────
	battery, err := fetchBatteryInfo(ctx, c, serial)
	if err != nil {
		battery = domain.BatteryInfo{}
	}

	// ── 4. Network ────────────────────────────────────────────────────────────
	network, err := fetchNetworkInfo(ctx, c, serial)
	if err != nil {
		network = domain.NetworkInfo{}
	}

	now := time.Now()
	return &domain.Device{
		Serial: serial,
		Info:   info,
		Display: display,
		State: domain.DeviceState{
			Status:  domain.StatusOnline,
			Battery: battery,
			Network: network,
		},
		ConnectedAt: now,
		LastSeen:    now,
	}, nil
}

// ── helpers ────────────────────────────────────────────────────────────────────

// getprop runs `adb shell getprop <key>` and returns the trimmed value.
func getprop(ctx context.Context, c Client, serial, key string) (string, error) {
	return c.Shell(ctx, serial, "getprop "+key)
}

func fetchDeviceInfo(ctx context.Context, c Client, serial string) (domain.DeviceInfo, error) {
	var info domain.DeviceInfo

	model, err := getprop(ctx, c, serial, "ro.product.model")
	if err != nil {
		return info, fmt.Errorf("ro.product.model: %w", err)
	}
	info.Model = model

	info.MarketName, _ = getprop(ctx, c, serial, "ro.product.name")
	info.Manufacturer, _ = getprop(ctx, c, serial, "ro.product.manufacturer")
	info.AndroidVersion, _ = getprop(ctx, c, serial, "ro.build.version.release")
	info.CPUABI, _ = getprop(ctx, c, serial, "ro.product.cpu.abi")

	sdkStr, _ := getprop(ctx, c, serial, "ro.build.version.sdk")
	if sdk, err := strconv.Atoi(sdkStr); err == nil {
		info.SDKVersion = sdk
	}

	// RAM: `cat /proc/meminfo | grep MemTotal`
	memOut, err := c.Shell(ctx, serial, "cat /proc/meminfo")
	if err == nil {
		info.RAMMB = parseMemTotal(memOut)
	}

	// Storage: `df /data`
	dfOut, err := c.Shell(ctx, serial, "df /data")
	if err == nil {
		info.StorageMB = parseDfTotal(dfOut)
	}

	return info, nil
}

// parseMemTotal extracts the MemTotal value (in MB) from /proc/meminfo output.
// Expected line: "MemTotal:       3813904 kB"
func parseMemTotal(out string) int64 {
	re := regexp.MustCompile(`MemTotal:\s+(\d+)\s+kB`)
	m := re.FindStringSubmatch(out)
	if len(m) < 2 {
		return 0
	}
	kb, _ := strconv.ParseInt(m[1], 10, 64)
	return kb / 1024
}

// parseDfTotal extracts total disk size (in MB) from `df /data` output.
// Works with both the 1K-block and 512-byte block variants Android may return.
func parseDfTotal(out string) int64 {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "/data") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			// fields[1] is the 1K block count (most Android builds)
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				continue
			}
			return kb / 1024
		}
	}
	return 0
}

// ── display ───────────────────────────────────────────────────────────────────

// fetchDisplayInfo runs `wm size`, `wm density`, and `dumpsys display` (brief).
func fetchDisplayInfo(ctx context.Context, c Client, serial string) (domain.DisplayInfo, error) {
	var d domain.DisplayInfo

	// wm size → "Physical size: 1080x2340"
	sizeOut, err := c.Shell(ctx, serial, "wm size")
	if err != nil {
		return d, err
	}
	d.Width, d.Height = parseWMSize(sizeOut)

	// wm density → "Physical density: 420"
	densOut, _ := c.Shell(ctx, serial, "wm density")
	d.Density = parseWMDensity(densOut)

	// FPS from dumpsys display (best effort)
	dispOut, _ := c.Shell(ctx, serial, "dumpsys display | grep -i 'fps\\|refresh'")
	d.Fps = parseFPS(dispOut)
	if d.Fps == 0 {
		d.Fps = 60 // sensible default
	}

	return d, nil
}

// parseWMSize extracts width and height from `wm size` output.
// Handles both "Physical size: WxH" and "Override size: WxH" lines.
var wmSizeRe = regexp.MustCompile(`(?i)size:\s*(\d+)x(\d+)`)

func parseWMSize(out string) (width, height int32) {
	m := wmSizeRe.FindStringSubmatch(out)
	if len(m) < 3 {
		return 0, 0
	}
	w, _ := strconv.ParseInt(m[1], 10, 32)
	h, _ := strconv.ParseInt(m[2], 10, 32)
	return int32(w), int32(h)
}

var wmDensityRe = regexp.MustCompile(`(?i)density:\s*(\d+)`)

func parseWMDensity(out string) int32 {
	m := wmDensityRe.FindStringSubmatch(out)
	if len(m) < 2 {
		return 0
	}
	d, _ := strconv.ParseInt(m[1], 10, 32)
	return int32(d)
}

var fpsRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*fps`)

func parseFPS(out string) int32 {
	m := fpsRe.FindStringSubmatch(strings.ToLower(out))
	if len(m) < 2 {
		return 0
	}
	f, _ := strconv.ParseFloat(m[1], 32)
	return int32(f)
}

// ── battery ───────────────────────────────────────────────────────────────────

// fetchBatteryInfo parses `dumpsys battery` output.
func fetchBatteryInfo(ctx context.Context, c Client, serial string) (domain.BatteryInfo, error) {
	out, err := c.Shell(ctx, serial, "dumpsys battery")
	if err != nil {
		return domain.BatteryInfo{}, err
	}
	return parseBattery(out), nil
}

func parseBattery(out string) domain.BatteryInfo {
	var b domain.BatteryInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "level:"):
			b.Level, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "level:")))
		case strings.HasPrefix(line, "AC powered:"), strings.HasPrefix(line, "USB powered:"):
			if strings.Contains(line, "true") {
				b.IsCharging = true
			}
		case strings.HasPrefix(line, "health:"):
			b.Health = healthString(strings.TrimSpace(strings.TrimPrefix(line, "health:")))
		}
	}
	return b
}

func healthString(code string) string {
	switch code {
	case "2":
		return "good"
	case "3":
		return "overheat"
	case "4":
		return "dead"
	case "5":
		return "over-voltage"
	case "6":
		return "unspecified-failure"
	case "7":
		return "cold"
	default:
		return "unknown"
	}
}

// ── network ───────────────────────────────────────────────────────────────────

// fetchNetworkInfo collects Wi-Fi SSID, IP address, mobile-data, and airplane state.
func fetchNetworkInfo(ctx context.Context, c Client, serial string) (domain.NetworkInfo, error) {
	var n domain.NetworkInfo

	// Wi-Fi SSID
	ssidOut, _ := c.Shell(ctx, serial, "dumpsys wifi | grep -E 'mWifiInfo|SSID'")
	n.WiFiSSID = parseSSID(ssidOut)
	n.Connected = n.WiFiSSID != "" && n.WiFiSSID != "<unknown ssid>"

	// IP address (first non-loopback IPv4 on wlan0)
	ipOut, _ := c.Shell(ctx, serial, "ip addr show wlan0")
	n.IP = parseIP(ipOut)

	// Mobile data
	mobOut, _ := c.Shell(ctx, serial, "settings get global mobile_data")
	n.MobileData = strings.TrimSpace(mobOut) == "1"

	// Airplane mode
	airOut, _ := c.Shell(ctx, serial, "settings get global airplane_mode_on")
	n.Airplane = strings.TrimSpace(airOut) == "1"

	return n, nil
}

var ssidRe = regexp.MustCompile(`SSID:\s*"([^"]*)"`)

func parseSSID(out string) string {
	m := ssidRe.FindStringSubmatch(out)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

var ipv4Re = regexp.MustCompile(`inet\s+(\d+\.\d+\.\d+\.\d+)/`)

func parseIP(out string) string {
	m := ipv4Re.FindStringSubmatch(out)
	if len(m) < 2 {
		return ""
	}
	ip := net.ParseIP(m[1])
	if ip == nil || ip.IsLoopback() {
		return ""
	}
	return ip.String()
}