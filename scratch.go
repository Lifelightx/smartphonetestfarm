package main

import (
	"fmt"
	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/mp4"
)

func main() {
	sps := []byte{0x67, 0x42, 0xc0, 0x15, 0xd9, 0x01, 0x41, 0xfb, 0x0e, 0x10, 0x00, 0x00, 0x3e, 0x90, 0x00, 0x0e, 0xa6, 0x08, 0xf1, 0x42, 0x94}
	pps := []byte{0x68, 0xce, 0x38, 0x80}

	parsedSps, err := avc.ParseSPSNALUnit(sps, true)
	if err != nil {
		fmt.Printf("ParseSPSNALUnit error: %v\n", err)
	} else {
		fmt.Printf("Parsed SPS: Width=%d, Height=%d\n", parsedSps.Width, parsedSps.Height)
	}

	avcC, err := mp4.CreateAvcC([][]byte{sps}, [][]byte{pps}, true)
	if err != nil {
		fmt.Printf("CreateAvcC error: %v\n", err)
	} else {
		fmt.Printf("CreateAvcC success: %v\n", avcC)
	}
}
