package stream

import (
	"bytes"
	"testing"

	"protean-provider/internal/config"
)

// ── H.264 parser tests ────────────────────────────────────────────────────────

func TestAnnexBSplit_Basic(t *testing.T) {
	// Two NAL units separated by 4-byte start code
	nal1 := []byte{0x67, 0x01, 0x02} // SPS (0x67 & 0x1F = 7)
	nal2 := []byte{0x68, 0x03, 0x04} // PPS (0x68 & 0x1F = 8)

	sc := []byte{0x00, 0x00, 0x00, 0x01}
	input := append(sc, nal1...)
	input = append(input, sc...)
	input = append(input, nal2...)

	units := annexBSplit(input)
	if len(units) != 2 {
		t.Fatalf("expected 2 NAL units, got %d", len(units))
	}
	if !bytes.Equal(units[0], nal1) {
		t.Errorf("unit 0: got %v, want %v", units[0], nal1)
	}
	if !bytes.Equal(units[1], nal2) {
		t.Errorf("unit 1: got %v, want %v", units[1], nal2)
	}
}

func TestExtractSPSPPS(t *testing.T) {
	spsData := []byte{0x67, 0x42, 0xC0, 0x1E, 0xAB} // NAL type 7 (SPS)
	ppsData := []byte{0x68, 0xCE, 0x38, 0x80}         // NAL type 8 (PPS)
	sc := []byte{0x00, 0x00, 0x00, 0x01}

	buf := append(sc, spsData...)
	buf = append(buf, sc...)
	buf = append(buf, ppsData...)

	sps, pps := extractSPSPPS(buf)
	if sps == nil {
		t.Fatal("expected SPS, got nil")
	}
	if pps == nil {
		t.Fatal("expected PPS, got nil")
	}
	if !bytes.Equal(sps, spsData) {
		t.Errorf("SPS mismatch: got %v want %v", sps, spsData)
	}
	if !bytes.Equal(pps, ppsData) {
		t.Errorf("PPS mismatch: got %v want %v", pps, ppsData)
	}
}

func TestFrameToAVCC_StripsParamSets(t *testing.T) {
	sc := []byte{0x00, 0x00, 0x00, 0x01}
	sps := []byte{0x67, 0x42, 0xC0, 0x1E}
	pps := []byte{0x68, 0xCE}
	idr := []byte{0x65, 0xB8, 0x00, 0x04}

	buf := append(sc, sps...)
	buf = append(buf, sc...)
	buf = append(buf, pps...)
	buf = append(buf, sc...)
	buf = append(buf, idr...)

	avcc := frameToAVCC(buf)
	// SPS and PPS must be stripped; only IDR NAL should be present
	if len(avcc) == 0 {
		t.Fatal("expected AVCC output, got empty")
	}
	// AVCC = 4-byte length + NAL data
	if len(avcc) < 4 {
		t.Fatalf("AVCC too short: %d bytes", len(avcc))
	}
	// The first 4 bytes are the length of the IDR NAL
	if !bytes.Equal(avcc[4:], idr) {
		t.Errorf("expected IDR NAL in AVCC, got %v", avcc[4:])
	}
}

// ── fMP4 muxer tests ──────────────────────────────────────────────────────────

func TestBuildInitSegment_NonEmpty(t *testing.T) {
	// Minimal but structurally valid SPS (profile=0x42, compat=0xC0, level=0x1E)
	sps := []byte{0x67, 0x42, 0xC0, 0x1E, 0xAB, 0x40}
	pps := []byte{0x68, 0xCE, 0x38, 0x80}

	init := BuildInitSegment(sps, pps, 15)
	if len(init) == 0 {
		t.Fatal("init segment is empty")
	}

	// Must start with 'ftyp'
	if string(init[4:8]) != "ftyp" {
		t.Errorf("expected ftyp box first, got %q", string(init[4:8]))
	}

	// Must contain 'moov' box
	if !bytes.Contains(init, []byte("moov")) {
		t.Error("init segment missing moov box")
	}

	// Must contain 'avcC'
	if !bytes.Contains(init, []byte("avcC")) {
		t.Error("init segment missing avcC box")
	}
}

func TestBuildMediaSegment_NonEmpty(t *testing.T) {
	sample := MediaSample{
		Data:     []byte{0x00, 0x00, 0x00, 0x04, 0x65, 0xB8, 0x00, 0x04}, // 4-byte len + IDR
		IsKey:    true,
		Duration: 6000, // 90000/15
	}

	seg := BuildMediaSegment(1, 0, []MediaSample{sample})
	if len(seg) == 0 {
		t.Fatal("media segment is empty")
	}

	// Must contain moof and mdat
	if !bytes.Contains(seg, []byte("moof")) {
		t.Error("media segment missing moof")
	}
	if !bytes.Contains(seg, []byte("mdat")) {
		t.Error("media segment missing mdat")
	}
}

// ── Manager tests ─────────────────────────────────────────────────────────────

func TestManager_IsCapturing_InitialState(t *testing.T) {
	cfg := &config.Config{}
	cfg.Stream.MaxFPS = 15
	cfg.Stream.Quality = 80
	m := NewManager(cfg)
	if m.IsCapturing("DUMMY001") {
		t.Error("expected IsCapturing=false for unknown serial")
	}
}
