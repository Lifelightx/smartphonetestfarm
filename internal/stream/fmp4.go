package stream

import (
	"bytes"
	"log/slog"

	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/mp4"
)

// mp4Timescale is standard for H.264 video.
const mp4Timescale = 90000

// BuildInitSegment builds the fMP4 initialization segment from raw SPS and PPS NAL units using mp4ff.
func BuildInitSegment(sps, pps []byte, maxFPS int) []byte {
	if maxFPS <= 0 {
		maxFPS = 15
	}
	init := mp4.CreateEmptyInit()
	init.AddEmptyTrack(mp4Timescale, "video", "eng")
	trak := init.Moov.Trak

	width, height := uint16(1080), uint16(1920)
	parsedSps, err := avc.ParseSPSNALUnit(sps, true)
	if err == nil {
		width = uint16(parsedSps.Width)
		height = uint16(parsedSps.Height)
	}

	avcC, err := mp4.CreateAvcC([][]byte{sps}, [][]byte{pps}, true)
	if err != nil {
		slog.Error("mp4ff: CreateAvcC error", "err", err)
	}

	avc1 := mp4.CreateVisualSampleEntryBox("avc1", width, height, avcC)
	stsd := trak.Mdia.Minf.Stbl.Stsd
	stsd.AddChild(avc1)

	trex := mp4.CreateTrex(1)
	trex.DefaultSampleDuration = uint32(mp4Timescale / maxFPS)
	trex.DefaultSampleFlags = 0x01010000

	mvex := mp4.NewMvexBox()
	mvex.AddChild(trex)
	init.Moov.AddChild(mvex)

	buf := bytes.Buffer{}
	if err := init.Encode(&buf); err != nil {
		slog.Error("mp4ff: Encode Init error", "err", err)
	}
	return buf.Bytes()
}

// MediaSample is one encoded video sample (AVCC format).
type MediaSample struct {
	Data     []byte // AVCC-wrapped NAL units
	IsKey    bool
	Duration uint32 // in mp4Timescale units
}

// BuildMediaSegment builds one fMP4 media segment using mp4ff.
func BuildMediaSegment(seqNum uint32, baseDecodeTime uint64, samples []MediaSample) []byte {
	frag, err := mp4.CreateFragment(seqNum, 1)
	if err != nil {
		slog.Error("mp4ff: CreateFragment error", "err", err)
		return nil
	}

	for i, s := range samples {
		var flags uint32
		if i == 0 {
			if s.IsKey {
				flags = 0x02000000 // depends_on=2
			} else {
				flags = 0x01010000 // depends_on=1, is_non_sync
			}
		} else {
			flags = 0x01010000
		}

		fullSamp := mp4.FullSample{
			Sample: mp4.Sample{
				Flags: flags,
				Dur:   s.Duration,
				Size:  uint32(len(s.Data)),
			},
			DecodeTime: baseDecodeTime,
			Data:       s.Data,
		}

		err = frag.AddFullSampleToTrack(fullSamp, 1)
		if err != nil {
			slog.Error("mp4ff: AddFullSampleToTrack error", "err", err)
		}
		baseDecodeTime += uint64(s.Duration)
	}

	buf := bytes.Buffer{}
	if err := frag.Encode(&buf); err != nil {
		slog.Error("mp4ff: Encode Fragment error", "err", err)
	}
	return buf.Bytes()
}
