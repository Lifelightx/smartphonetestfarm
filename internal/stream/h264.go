package stream

import "encoding/binary"

// NAL unit types (H.264 spec Table 7-1)
const (
	nalNonIDR byte = 1
	nalIDR    byte = 5
	nalSEI    byte = 6
	nalSPS    byte = 7
	nalPPS    byte = 8
	nalAUD    byte = 9
)

func nalType(b byte) byte { return b & 0x1F }

// annexBSplit splits an Annex-B H.264 byte stream into individual raw NAL units
// (start codes stripped). Empty slices are discarded.
func annexBSplit(data []byte) [][]byte {
	var units [][]byte
	n := len(data)
	start := -1

	findStart := func(i int) (pos, skip int, found bool) {
		if i+3 < n && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			return i, 4, true
		}
		if i+2 < n && data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			return i, 3, true
		}
		return 0, 0, false
	}

	i := 0
	for i < n {
		pos, skip, found := findStart(i)
		if found {
			if start >= 0 && pos > start {
				units = append(units, data[start:pos])
			}
			i = pos + skip
			start = i
		} else {
			i++
		}
	}
	if start >= 0 && start < n {
		units = append(units, data[start:])
	}
	return units
}

// avccWrap wraps a single raw NAL unit with a 4-byte big-endian length prefix (AVCC).
func avccWrap(nal []byte) []byte {
	b := make([]byte, 4+len(nal))
	binary.BigEndian.PutUint32(b[:4], uint32(len(nal)))
	copy(b[4:], nal)
	return b
}

// frameToAVCC converts one complete H.264 Annex-B access unit to AVCC format,
// stripping SPS, PPS, SEI, and AUD NAL units (those belong in the init segment).
func frameToAVCC(annexB []byte) []byte {
	nals := annexBSplit(annexB)
	var out []byte
	for _, nal := range nals {
		if len(nal) == 0 {
			continue
		}
		switch nalType(nal[0]) {
		case nalSPS, nalPPS, nalSEI, nalAUD:
			// Skip SPS, PPS, SEI, AUD not needed in mdat
		default:
			out = append(out, avccWrap(nal)...)
		}
	}
	return out
}

// extractSPSPPS scans an Annex-B buffer and returns the first SPS and PPS NAL units found.
func extractSPSPPS(annexB []byte) (sps, pps []byte) {
	for _, nal := range annexBSplit(annexB) {
		if len(nal) == 0 {
			continue
		}
		switch nalType(nal[0]) {
		case nalSPS:
			if sps == nil {
				sps = nal
			}
		case nalPPS:
			if pps == nil {
				pps = nal
			}
		}
	}
	return
}

// scrcpyFrame holds one parsed frame from the scrcpy-server wire protocol.
// When send_frame_meta=true, each frame is preceded by:
//
//	8 bytes: PTS in microseconds (big-endian int64)
//	4 bytes: data length (big-endian uint32)
type scrcpyFrame struct {
	PTS  int64  // microseconds
	Data []byte // H.264 Annex-B
}

type bitReader struct {
	data    []byte
	bytePos int
	bitPos  int
}

func (r *bitReader) readBit() uint {
	if r.bytePos >= len(r.data) {
		return 0
	}
	bit := (r.data[r.bytePos] >> (7 - r.bitPos)) & 1
	r.bitPos++
	if r.bitPos == 8 {
		r.bitPos = 0
		r.bytePos++
	}
	return uint(bit)
}

func (r *bitReader) readBits(n int) uint {
	var val uint
	for i := 0; i < n; i++ {
		val = (val << 1) | r.readBit()
	}
	return val
}

func (r *bitReader) readUE() uint {
	var zeroCount int
	for r.readBit() == 0 {
		zeroCount++
	}
	if zeroCount == 0 {
		return 0
	}
	val := r.readBits(zeroCount)
	return (1 << zeroCount) - 1 + val
}

func (r *bitReader) readSE() int {
	val := r.readUE()
	if val%2 == 0 {
		return -int(val / 2)
	}
	return int((val + 1) / 2)
}

// parseSPS parses raw SPS NAL unit (excluding any start codes) and returns width and height.
func parseSPS(sps []byte) (width, height uint16) {
	// First check and strip emulation prevention three bytes (0x00, 0x00, 0x03)
	var data []byte
	for i := 0; i < len(sps); i++ {
		if i+2 < len(sps) && sps[i] == 0 && sps[i+1] == 0 && sps[i+2] == 3 {
			data = append(data, sps[i], sps[i+1])
			i += 2
		} else {
			data = append(data, sps[i])
		}
	}

	if len(data) < 5 {
		return 0, 0
	}

	r := &bitReader{data: data[4:], bytePos: 0, bitPos: 0} // Skip NAL header, profile, constraints, level

	profileIdc := data[1]
	r.readUE() // seq_parameter_set_id

	if profileIdc == 100 || profileIdc == 110 || profileIdc == 122 || profileIdc == 244 ||
		profileIdc == 44 || profileIdc == 83 || profileIdc == 86 || profileIdc == 118 ||
		profileIdc == 128 || profileIdc == 138 || profileIdc == 139 || profileIdc == 144 {
		chromaFormatIdc := r.readUE()
		if chromaFormatIdc == 3 {
			r.readBit() // separate_colour_plane_flag
		}
		r.readUE() // bit_depth_luma_minus8
		r.readUE() // bit_depth_chroma_minus8
		r.readBit() // qpprime_y_zero_transform_bypass_flag
		seqScalingMatrixPresentFlag := r.readBit()
		if seqScalingMatrixPresentFlag != 0 {
			var limit int
			if chromaFormatIdc != 3 {
				limit = 8
			} else {
				limit = 12
			}
			for i := 0; i < limit; i++ {
				seqScalingListPresentFlag := r.readBit()
				if seqScalingListPresentFlag != 0 {
					sizeOfScalingList := 16
					if i >= 6 {
						sizeOfScalingList = 64
					}
					lastScale := 8
					nextScale := 8
					for j := 0; j < sizeOfScalingList; j++ {
						if nextScale != 0 {
							deltaScale := int(r.readSE())
							nextScale = (lastScale + deltaScale + 256) % 256
						}
						if nextScale != 0 {
							lastScale = nextScale
						}
					}
				}
			}
		}
	}

	r.readUE() // log2_max_frame_num_minus4
	picOrderCntType := r.readUE()
	if picOrderCntType == 0 {
		r.readUE() // log2_max_pic_order_cnt_lsb_minus4
	} else if picOrderCntType == 1 {
		r.readBit() // delta_pic_order_always_zero_flag
		r.readUE() // offset_for_non_ref_pic
		r.readUE() // offset_for_top_to_bottom_field
		numRefFramesInPicOrderCntCycle := r.readUE()
		for i := uint(0); i < numRefFramesInPicOrderCntCycle; i++ {
			r.readUE() // offset_for_ref_frame
		}
	}
	r.readUE() // max_num_ref_frames
	r.readBit() // gaps_in_frame_num_value_allowed_flag

	picWidthInMbsMinus1 := r.readUE()
	picHeightInMapUnitsMinus1 := r.readUE()
	frameMbsOnlyFlag := r.readBit()
	if frameMbsOnlyFlag == 0 {
		r.readBit() // mb_adaptive_frame_field_flag
	}
	r.readBit() // direct_8x8_inference_flag
	frameCroppingFlag := r.readBit()
	var cropLeft, cropRight, cropTop, cropBottom uint
	if frameCroppingFlag != 0 {
		cropLeft = r.readUE()
		cropRight = r.readUE()
		cropTop = r.readUE()
		cropBottom = r.readUE()
	}
	r.readBit() // vui_parameters_present_flag

	width = uint16((picWidthInMbsMinus1 + 1) * 16)
	height = uint16((picHeightInMapUnitsMinus1 + 1) * 16)
	if frameMbsOnlyFlag == 0 {
		height *= 2
	}

	// Adjust for cropping
	var cropUnitX, cropUnitY uint = 2, 2 // Default YUV420
	width -= uint16((cropLeft + cropRight) * cropUnitX)
	height -= uint16((cropTop + cropBottom) * cropUnitY)

	return width, height
}
