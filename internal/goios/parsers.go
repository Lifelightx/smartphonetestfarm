package goios

import (
	"encoding/json"
	"fmt"
	"strings"
)

// parseIOSList parses the output of `ios list` command, handling potential warning/log lines
// or alternative output shapes (array, object with deviceList key, single device object).
func parseIOSList(outputStr string) ([]IOSDeviceEntry, error) {
	type DeviceListWrapper struct {
		DeviceList []IOSDeviceEntry `json:"deviceList"`
	}
	type DeviceListWrapperStrings struct {
		DeviceList []string `json:"deviceList"`
	}

	// Helper to convert []string of UDIDs to []IOSDeviceEntry
	toEntries := func(udids []string) []IOSDeviceEntry {
		var entries []IOSDeviceEntry
		for _, udid := range udids {
			entries = append(entries, IOSDeviceEntry{UDID: udid})
		}
		return entries
	}

	// 1. Try parsing the entire output as a DeviceListWrapper
	var wrapper DeviceListWrapper
	if err := json.Unmarshal([]byte(outputStr), &wrapper); err == nil && wrapper.DeviceList != nil {
		return wrapper.DeviceList, nil
	}

	// 1b. Try parsing the entire output as DeviceListWrapperStrings
	var wrapperStrings DeviceListWrapperStrings
	if err := json.Unmarshal([]byte(outputStr), &wrapperStrings); err == nil && wrapperStrings.DeviceList != nil {
		return toEntries(wrapperStrings.DeviceList), nil
	}

	// 2. Try parsing the entire output as a JSON array of devices
	var list []IOSDeviceEntry
	if err := json.Unmarshal([]byte(outputStr), &list); err == nil {
		return list, nil
	}

	// 2b. Try parsing the entire output as a JSON array of UDID strings
	var stringList []string
	if err := json.Unmarshal([]byte(outputStr), &stringList); err == nil {
		return toEntries(stringList), nil
	}

	// 3. Try parsing the entire output as a single device
	var single IOSDeviceEntry
	if err := json.Unmarshal([]byte(outputStr), &single); err == nil && single.UDID != "" {
		return []IOSDeviceEntry{single}, nil
	}

	// 4. Try parsing line-by-line (handles log line prefix / NDJSON)
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Try parsing this line as DeviceListWrapper
		var w DeviceListWrapper
		if err := json.Unmarshal([]byte(line), &w); err == nil && w.DeviceList != nil {
			return w.DeviceList, nil
		}

		// Try parsing this line as DeviceListWrapperStrings
		var wStr DeviceListWrapperStrings
		if err := json.Unmarshal([]byte(line), &wStr); err == nil && wStr.DeviceList != nil {
			return toEntries(wStr.DeviceList), nil
		}

		// Try parsing this line as a JSON array of devices
		var lst []IOSDeviceEntry
		if err := json.Unmarshal([]byte(line), &lst); err == nil {
			return lst, nil
		}

		// Try parsing this line as a JSON array of UDID strings
		var sList []string
		if err := json.Unmarshal([]byte(line), &sList); err == nil {
			return toEntries(sList), nil
		}

		// Try parsing this line as a single device
		var s IOSDeviceEntry
		if err := json.Unmarshal([]byte(line), &s); err == nil && s.UDID != "" {
			return []IOSDeviceEntry{s}, nil
		}
	}

	// 5. If everything else failed, but there is a "[" or "{" in the output,
	// try to isolate it and parse (original fallback behavior)
	idx := strings.Index(outputStr, "[")
	if idx != -1 {
		var lst []IOSDeviceEntry
		if err := json.Unmarshal([]byte(outputStr[idx:]), &lst); err == nil {
			return lst, nil
		}
		var sList []string
		if err := json.Unmarshal([]byte(outputStr[idx:]), &sList); err == nil {
			return toEntries(sList), nil
		}
	}

	singleIdx := strings.Index(outputStr, "{")
	if singleIdx != -1 {
		var s IOSDeviceEntry
		if err := json.Unmarshal([]byte(outputStr[singleIdx:]), &s); err == nil && s.UDID != "" {
			return []IOSDeviceEntry{s}, nil
		}
	}

	return nil, fmt.Errorf("failed to parse ios list: invalid output format")
}

// parseJSONMap parses a JSON map from command output, skipping warning/log lines
// that might be mixed in (e.g. from the go-ios agent not running).
func parseJSONMap(outputStr string) (map[string]interface{}, error) {
	// 1. Try parsing entire output
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(outputStr), &m); err == nil {
		return m, nil
	}

	// 2. Try line-by-line
	var logMap map[string]interface{}
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var temp map[string]interface{}
		if err := json.Unmarshal([]byte(line), &temp); err == nil {
			// Check if it's a log/warning line
			_, hasLevel := temp["level"]
			_, hasMsg := temp["msg"]
			if hasLevel && hasMsg {
				logMap = temp
				continue
			}
			return temp, nil
		}
	}

	if logMap != nil {
		return logMap, nil
	}

	// 3. Fallback to extracting first `{`
	idx := strings.Index(outputStr, "{")
	if idx != -1 {
		var temp map[string]interface{}
		if err := json.Unmarshal([]byte(outputStr[idx:]), &temp); err == nil {
			return temp, nil
		}
	}

	return nil, fmt.Errorf("failed to parse JSON map")
}

// parseIOSAppList parses the output of `ios apps` command, handling potential warning/log lines.
func parseIOSAppList(outputStr string) ([]IOSAppInfo, map[string]interface{}, error) {
	// 1. Try parsing entire output as array of IOSAppInfo
	var apps []IOSAppInfo
	if err := json.Unmarshal([]byte(outputStr), &apps); err == nil {
		return apps, nil, nil
	}

	// 2. Try parsing entire output as a map of apps
	var mapApps map[string]interface{}
	if err := json.Unmarshal([]byte(outputStr), &mapApps); err == nil {
		return nil, mapApps, nil
	}

	// 3. Try parsing line-by-line
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Try parsing this line as a JSON array of apps
		var lst []IOSAppInfo
		if err := json.Unmarshal([]byte(line), &lst); err == nil {
			return lst, nil, nil
		}

		// Try parsing this line as a map of apps
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err == nil {
			// Check if it's a warning/log line
			_, hasLevel := m["level"]
			_, hasMsg := m["msg"]
			if hasLevel && hasMsg {
				continue
			}
			return nil, m, nil
		}
	}

	// 4. Fallbacks
	idx := strings.Index(outputStr, "[")
	if idx != -1 {
		if err := json.Unmarshal([]byte(outputStr[idx:]), &apps); err == nil {
			return apps, nil, nil
		}
	}

	mapIdx := strings.Index(outputStr, "{")
	if mapIdx != -1 {
		if err := json.Unmarshal([]byte(outputStr[mapIdx:]), &mapApps); err == nil {
			return nil, mapApps, nil
		}
	}

	return nil, nil, fmt.Errorf("failed to parse apps list")
}
