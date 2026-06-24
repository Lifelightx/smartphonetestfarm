package stream

import (
	"encoding/binary"
	"log/slog"
)

// fMP4 (Fragmented MP4) minimal muxer for a single H.264 video track.
//
// Layout:
//   Init segment: ftyp + moov  (sent once per WebSocket/HTTP client)
//   Media segment: moof + mdat  (sent per frame or per keyframe group)
//
// Timescale is fixed at 90000 (standard for H.264 video).

const mp4Timescale = 90000

// ── Box helpers ───────────────────────────────────────────────────────────────

func put32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func put16(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

func put64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// mkBox builds: [4-byte size][4-byte type][payload...]
func mkBox(typ string, payload ...[]byte) []byte {
	var total int
	for _, p := range payload {
		total += len(p)
	}
	out := make([]byte, 8, 8+total)
	binary.BigEndian.PutUint32(out[:4], uint32(8+total))
	copy(out[4:8], typ)
	for _, p := range payload {
		out = append(out, p...)
	}
	return out
}

// mkFullBox builds: [4-byte size][4-byte type][1-byte version][3-byte flags][payload...]
func mkFullBox(typ string, version byte, flags uint32, payload ...[]byte) []byte {
	hdr := make([]byte, 4)
	hdr[0] = version
	hdr[1] = byte(flags >> 16)
	hdr[2] = byte(flags >> 8)
	hdr[3] = byte(flags)
	args := append([][]byte{hdr}, payload...)
	return mkBox(typ, args...)
}

// ── Init segment ──────────────────────────────────────────────────────────────

// BuildInitSegment builds the fMP4 initialization segment from raw SPS and PPS NAL units.
// This must be sent to the client before any media segments.
func BuildInitSegment(sps, pps []byte, maxFPS int) []byte {
	if maxFPS <= 0 {
		maxFPS = 15
	}
	ftyp := buildFtyp()
	moov := buildMoov(sps, pps, maxFPS)
	out := make([]byte, 0, len(ftyp)+len(moov))
	out = append(out, ftyp...)
	out = append(out, moov...)
	return out
}

func buildFtyp() []byte {
	return mkBox("ftyp",
		[]byte("isom"),      // major brand
		put32(0x00000001),   // minor version
		[]byte("isom"),      // compatible: ISO Base Media
		[]byte("iso6"),      // compatible: ISO Base Media v6
		[]byte("msdh"),      // compatible: Media Segment
		[]byte("avc1"),      // compatible: AVC
	)
}

func buildMoov(sps, pps []byte, maxFPS int) []byte {
	const trackID = 1
	// duration=0 for live/fragmented
	mvhd := mkFullBox("mvhd", 0, 0,
		put32(0),              // creation_time
		put32(0),              // modification_time
		put32(mp4Timescale),   // timescale
		put32(0),              // duration = 0 (live)
		put32(0x00010000),     // rate = 1.0
		put16(0x0100),         // volume = 1.0
		make([]byte, 10),      // reserved
		// unity matrix
		put32(0x00010000), put32(0), put32(0),
		put32(0), put32(0x00010000), put32(0),
		put32(0), put32(0), put32(0x40000000),
		make([]byte, 24), // pre_defined
		put32(2),         // next_track_ID = 2
	)

	trak := buildTrak(sps, pps, trackID, maxFPS)
	mvex := buildMvex(trackID, maxFPS)

	return mkBox("moov", mvhd, trak, mvex)
}

func buildTrak(sps, pps []byte, trackID, maxFPS int) []byte {
	width, height := parseSPS(sps)
	slog.Info("video metadata", "width", width, "height", height)
	if width == 0 || height == 0 {
		width, height = 1080, 1920
	}
	tkhd := mkFullBox("tkhd", 0, 3, // flags: track enabled + in movie
		put32(0),          // creation_time
		put32(0),          // modification_time
		put32(uint32(trackID)),
		put32(0),          // reserved
		put32(0),          // duration = 0 (live)
		make([]byte, 8),   // reserved
		put16(0),          // layer
		put16(0),          // alternate_group
		put16(0),          // volume = 0 (video track)
		put16(0),          // reserved
		// unity matrix
		put32(0x00010000), put32(0), put32(0),
		put32(0), put32(0x00010000), put32(0),
		put32(0), put32(0), put32(0x40000000),
		put32(uint32(width)<<16),  // width
		put32(uint32(height)<<16), // height
	)

	mdia := buildMdia(sps, pps, maxFPS)
	return mkBox("trak", tkhd, mdia)
}

func buildMdia(sps, pps []byte, maxFPS int) []byte {
	mdhd := mkFullBox("mdhd", 0, 0,
		put32(0),            // creation_time
		put32(0),            // modification_time
		put32(mp4Timescale), // timescale
		put32(0),            // duration = 0
		put16(0x55C4),       // language = und (0x55C4 = ISO 639-2/T "und")
		put16(0),            // pre_defined
	)

	hdlr := mkFullBox("hdlr", 0, 0,
		put32(0),          // pre_defined
		[]byte("vide"),    // handler_type
		make([]byte, 12),  // reserved
		[]byte("VideoHandler\x00"),
	)

	minf := buildMinf(sps, pps)
	return mkBox("mdia", mdhd, hdlr, minf)
}

func buildMinf(sps, pps []byte) []byte {
	vmhd := mkFullBox("vmhd", 0, 1, make([]byte, 4)) // graphicsMode=0, opcolor=0

	dinf := mkBox("dinf",
		mkFullBox("dref", 0, 0,
			put32(1), // entry_count = 1
			mkFullBox("url ", 0, 1), // self-contained
		),
	)

	stbl := buildStbl(sps, pps)
	return mkBox("minf", vmhd, dinf, stbl)
}

func buildStbl(sps, pps []byte) []byte {
	avcC := buildAvcC(sps, pps)
	width, height := parseSPS(sps)
	if width == 0 || height == 0 {
		width, height = 1080, 1920
	}
	avc1 := mkBox("avc1",
		make([]byte, 6),   // reserved
		put16(1),          // data_reference_index = 1
		make([]byte, 16),  // pre_defined + reserved
		put16(width),      // width
		put16(height),     // height
		put32(0x00480000), // horizresolution = 72dpi
		put32(0x00480000), // vertresolution  = 72dpi
		put32(0),          // reserved
		put16(1),          // frame_count = 1
		make([]byte, 32),  // compressorname (empty)
		put16(0x0018),     // depth = 24
		[]byte{0xFF, 0xFF}, // pre_defined = -1
		avcC,
	)

	stsd := mkFullBox("stsd", 0, 0, put32(1), avc1)

	// All other sample table boxes are empty for fragmented MP4
	stts := mkFullBox("stts", 0, 0, put32(0))
	stsc := mkFullBox("stsc", 0, 0, put32(0))
	stsz := mkFullBox("stsz", 0, 0, put32(0), put32(0))
	stco := mkFullBox("stco", 0, 0, put32(0))

	return mkBox("stbl", stsd, stts, stsc, stsz, stco)
}

func buildAvcC(sps, pps []byte) []byte {
	// AVCDecoderConfigurationRecord (ISO 14496-15 §5.3.3.1.2)
	payload := []byte{
		0x01,              // configurationVersion = 1
		sps[1],            // AVCProfileIndication
		sps[2],            // profile_compatibility
		sps[3],            // AVCLevelIndication
		0xFF,              // lengthSizeMinusOne = 3 → 4-byte length prefix
		0xE1,              // numSequenceParameterSets = 1
	}
	payload = append(payload, put16(uint16(len(sps)))...)
	payload = append(payload, sps...)
	payload = append(payload, 0x01) // numPictureParameterSets = 1
	payload = append(payload, put16(uint16(len(pps)))...)
	payload = append(payload, pps...)
	return mkBox("avcC", payload)
}

func buildMvex(trackID, maxFPS int) []byte {
	frameDuration := uint32(mp4Timescale / maxFPS)
	trex := mkFullBox("trex", 0, 0,
		put32(uint32(trackID)), // track_ID
		put32(1),               // default_sample_description_index = 1
		put32(frameDuration),   // default_sample_duration
		put32(0),               // default_sample_size = 0 (variable)
		put32(0x00010000),      // default_sample_flags (non-sync)
	)
	return mkBox("mvex", trex)
}

// ── Media segment ─────────────────────────────────────────────────────────────

// MediaSample is one encoded video sample (AVCC format).
type MediaSample struct {
	Data     []byte // AVCC-wrapped NAL units
	IsKey    bool
	Duration uint32 // in mp4Timescale units; 0 = use default from trex
}

// BuildMediaSegment builds one fMP4 media segment (moof + mdat).
//
//   - seqNum: monotonically increasing sequence number (starts at 1)
//   - baseDecodeTime: decode timestamp in mp4Timescale units
//   - samples: one or more video samples for this fragment
func BuildMediaSegment(seqNum uint32, baseDecodeTime uint64, samples []MediaSample) []byte {
	const trackID = 1

	// Build trun sample entries
	// trun flags: 0x000701 = data-offset-present | sample-duration-present |
	//             sample-size-present | sample-flags-present
	const trunFlags = 0x000701

	// Compute total mdat size
	var mdatPayload []byte
	for _, s := range samples {
		mdatPayload = append(mdatPayload, s.Data...)
	}

	// trun entries
	var trunEntries []byte
	for i, s := range samples {
		var flags uint32
		if i == 0 {
			if s.IsKey {
				flags = 0x02000000 // sample_depends_on=2 (key frame)
			} else {
				flags = 0x01010000 // sample_depends_on=1, sample_is_non_sync
			}
		}
		dur := s.Duration
		if dur == 0 {
			dur = 0 // 0 means use default from trex
		}
		trunEntries = append(trunEntries, put32(dur)...)      // sample_duration
		trunEntries = append(trunEntries, put32(uint32(len(s.Data)))...) // sample_size
		trunEntries = append(trunEntries, put32(flags)...)    // sample_flags
	}

	trun := mkFullBox("trun", 0, trunFlags,
		put32(uint32(len(samples))), // sample_count
		put32(0),                    // data_offset (placeholder, fixed up below)
		trunEntries,
	)

	tfdt := mkFullBox("tfdt", 1, 0, put64(baseDecodeTime))

	tfhd := mkFullBox("tfhd", 0, 0x020000, // default-base-is-moof
		put32(trackID),
	)

	traf := mkBox("traf", tfhd, tfdt, trun)
	mfhd := mkFullBox("mfhd", 0, 0, put32(seqNum))
	moof := mkBox("moof", mfhd, traf)
	mdat := mkBox("mdat", mdatPayload)

	// Fix up data_offset in trun: offset from start of moof to start of mdat payload.
	// data_offset position: moof(8) + mfhd(16) + traf(8) + tfhd(16) + tfdt(20) + trun(8+4) = ...
	// Easier: data_offset = len(moof) + 8  (mdat box header size)
	dataOffset := uint32(len(moof) + 8)
	// trun data_offset is at: inside trun box, after 8-byte box header + 4-byte version/flags + 4-byte sample_count = offset 16
	// Find it: scan moof for trun signature and patch
	patchTrunDataOffset(moof, dataOffset)

	out := make([]byte, 0, len(moof)+len(mdat))
	out = append(out, moof...)
	out = append(out, mdat...)
	return out
}

// patchTrunDataOffset finds the data_offset field inside a moof buffer and writes v into it.
// data_offset is the 4 bytes immediately after the trun sample_count field.
func patchTrunDataOffset(moof []byte, v uint32) {
	// Search for "trun" box type signature
	target := []byte("trun")
	for i := 0; i < len(moof)-16; i++ {
		if moof[i] == target[0] && moof[i+1] == target[1] && moof[i+2] == target[2] && moof[i+3] == target[3] {
			// After box type (4 bytes): version(1) + flags(3) + sample_count(4) + data_offset(4)
			offset := i + 4 + 4 + 4 // skip type + version/flags + sample_count
			if offset+4 <= len(moof) {
				binary.BigEndian.PutUint32(moof[offset:offset+4], v)
			}
			return
		}
	}
}
