package main

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

func main() {
	resp, err := http.Get("http://127.0.0.1:10401/")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		fmt.Printf("ParseMediaType Error: %v\n", err)
		return
	}
	boundary := strings.TrimPrefix(params["boundary"], "--")
	mr := multipart.NewReader(resp.Body, boundary)

	var lastBytes []byte
	for i := 0; i < 20; i++ {
		part, err := mr.NextPart()
		if err != nil {
			fmt.Printf("NextPart Error: %v\n", err)
			return
		}
		imgBytes, err := io.ReadAll(part)
		part.Close()
		if err != nil {
			fmt.Printf("ReadAll Error: %v\n", err)
			return
		}

		if len(lastBytes) > 0 {
			equal := bytes.Equal(imgBytes, lastBytes)
			fmt.Printf("Frame %d: len=%d, identical to last=%t\n", i, len(imgBytes), equal)
		} else {
			fmt.Printf("Frame %d: len=%d\n", i, len(imgBytes))
		}
		lastBytes = imgBytes
		time.Sleep(100 * time.Millisecond)
	}
}
