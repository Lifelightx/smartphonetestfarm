package stream

import (
	"encoding/binary"
	"fmt"
)

// ControlEvent represents a user control interaction event sent from the client.
type ControlEvent struct {
	Type      string  `json:"type"`
	Action    int     `json:"action"`   // 0=down, 1=up, 2=move
	X         float64 `json:"x"`        // normalized [0,1]
	Y         float64 `json:"y"`        // normalized [0,1]
	Pressure  float64 `json:"pressure"` // [0,1]
	Keycode   int     `json:"keycode"`
	HScroll   float64 `json:"hscroll"`
	VScroll   float64 `json:"vscroll"`
	Text      string  `json:"text"`
	Button    int     `json:"button"`
	Buttons   int     `json:"buttons"`
	PointerID int64   `json:"pointerId"`
	Path      string  `json:"path,omitempty"`
}

// SerializeControlEvent encodes a ControlEvent into raw scrcpy binary protocol messages.
// It returns a slice of messages because some events (like key click) map to multiple actions (down & up).
func SerializeControlEvent(ev *ControlEvent, vw, vh uint16) ([][]byte, error) {
	if vw == 0 || vh == 0 {
		vw, vh = 1080, 1920 // fallback
	}

	switch ev.Type {
	case "touch":
		if ev.Pressure == 0 && ev.Action == 0 {
			ev.Pressure = 1.0
		}
		var actionButton uint32
		var buttons uint32
		if ev.Buttons > 0 {
			if ev.Action == 0 || ev.Action == 1 {
				switch ev.Button {
				case 0:
					actionButton = 1 // BUTTON_PRIMARY
				case 1:
					actionButton = 4 // BUTTON_TERTIARY
				case 2:
					actionButton = 2 // BUTTON_SECONDARY
				}
			}
			if ev.Buttons&1 != 0 {
				buttons |= 1
			}
			if ev.Buttons&2 != 0 {
				buttons |= 2
			}
			if ev.Buttons&4 != 0 {
				buttons |= 4
			}
		} else {
			if ev.Action == 0 || ev.Action == 1 {
				actionButton = 1
				buttons = 1
			}
		}

		pointerID := ev.PointerID
		if ev.Buttons > 0 && pointerID == 0 {
			pointerID = -1
		}

		absX := int32(ev.X * float64(vw))
		absY := int32(ev.Y * float64(vh))
		pressure := uint16(ev.Pressure * 0xFFFF)

		// SC_CONTROL_MSG_TYPE_INJECT_TOUCH_EVENT = 2
		// struct: [1 type][1 action][8 pointer_id][4 x][4 y][2 w][2 h][2 pressure][4 actionButton][4 buttons]
		msg := make([]byte, 32)
		msg[0] = 2 // type: INJECT_TOUCH_EVENT
		msg[1] = byte(ev.Action)
		binary.BigEndian.PutUint64(msg[2:10], uint64(pointerID))
		binary.BigEndian.PutUint32(msg[10:14], uint32(absX))
		binary.BigEndian.PutUint32(msg[14:18], uint32(absY))
		binary.BigEndian.PutUint16(msg[18:20], vw)
		binary.BigEndian.PutUint16(msg[20:22], vh)
		binary.BigEndian.PutUint16(msg[22:24], pressure)
		binary.BigEndian.PutUint32(msg[24:28], actionButton)
		binary.BigEndian.PutUint32(msg[28:32], buttons)
		return [][]byte{msg}, nil

	case "scroll":
		absX := int32(ev.X * float64(vw))
		absY := int32(ev.Y * float64(vh))
		hVal := ev.HScroll
		if hVal > 1 {
			hVal = 1
		} else if hVal < -1 {
			hVal = -1
		}
		vVal := ev.VScroll
		if vVal > 1 {
			vVal = 1
		} else if vVal < -1 {
			vVal = -1
		}

		var hscroll int16
		if hVal == 1.0 {
			hscroll = 0x7fff
		} else {
			hscroll = int16(hVal * 32768.0)
		}
		var vscroll int16
		if vVal == 1.0 {
			vscroll = 0x7fff
		} else {
			vscroll = int16(vVal * 32768.0)
		}

		// SC_CONTROL_MSG_TYPE_INJECT_SCROLL_EVENT = 3
		// struct: [1 type][4 x][4 y][2 w][2 h][2 hscroll][2 vscroll][4 buttons]
		msg := make([]byte, 21)
		msg[0] = 3 // type: INJECT_SCROLL_EVENT
		binary.BigEndian.PutUint32(msg[1:5], uint32(absX))
		binary.BigEndian.PutUint32(msg[5:9], uint32(absY))
		binary.BigEndian.PutUint16(msg[9:11], vw)
		binary.BigEndian.PutUint16(msg[11:13], vh)
		binary.BigEndian.PutUint16(msg[13:15], uint16(hscroll))
		binary.BigEndian.PutUint16(msg[15:17], uint16(vscroll))
		binary.BigEndian.PutUint32(msg[17:21], 0)
		return [][]byte{msg}, nil

	case "text":
		textBytes := []byte(ev.Text)
		// SC_CONTROL_MSG_TYPE_INJECT_TEXT = 1
		// struct: [1 type][4 length][length string]
		msg := make([]byte, 1+4+len(textBytes))
		msg[0] = 1 // type: INJECT_TEXT
		binary.BigEndian.PutUint32(msg[1:5], uint32(len(textBytes)))
		copy(msg[5:], textBytes)
		return [][]byte{msg}, nil

	case "key":
		// SC_CONTROL_MSG_TYPE_INJECT_KEYCODE = 0
		// struct: [1 type][1 action][4 keycode][4 repeat][4 metastate]
		msgDown := make([]byte, 14)
		msgDown[0] = 0 // type: INJECT_KEYCODE
		msgDown[1] = 0 // action: down
		binary.BigEndian.PutUint32(msgDown[2:6], uint32(ev.Keycode))
		binary.BigEndian.PutUint32(msgDown[6:10], 0)
		binary.BigEndian.PutUint32(msgDown[10:14], 0)

		msgUp := make([]byte, 14)
		msgUp[0] = 0 // type: INJECT_KEYCODE
		msgUp[1] = 1 // action: up
		binary.BigEndian.PutUint32(msgUp[2:6], uint32(ev.Keycode))
		binary.BigEndian.PutUint32(msgUp[6:10], 0)
		binary.BigEndian.PutUint32(msgUp[10:14], 0)

		return [][]byte{msgDown, msgUp}, nil

	default:
		return nil, fmt.Errorf("unknown event type: %s", ev.Type)
	}
}
