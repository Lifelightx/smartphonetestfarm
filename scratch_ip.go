package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	fmt.Println("Testing IP detection steps...")

	// Step 2
	for _, env := range []string{"HOST_IP", "PROVIDER_IP", "PROVIDER_PROVIDER_IP"} {
		if val := os.Getenv(env); val != "" {
			fmt.Printf("Step 2: Found env %s = %s\n", env, val)
		}
	}

	// Step 3
	fmt.Println("Step 3: Dialing UDP 8.8.8.8:80...")
	t0 := time.Now()
	conn, err := net.Dial("udp", "8.8.8.8:80")
	fmt.Printf("Step 3 completed in %v, err: %v\n", time.Since(t0), err)
	if err == nil {
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		fmt.Printf("Step 3 IP: %s\n", localAddr.IP.String())
		conn.Close()
	}

	// Step 4
	fmt.Println("Step 4: Querying InterfaceAddrs...")
	t0 = time.Now()
	addrs, err := net.InterfaceAddrs()
	fmt.Printf("Step 4 completed in %v, err: %v\n", time.Since(t0), err)
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					fmt.Printf("Step 4 IP: %s\n", ipnet.IP.String())
				}
			}
		}
	}

	// Step 5
	fmt.Println("Step 5: LookupHost host.docker.internal...")
	t0 = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	dockerAddrs, dnsErr := net.DefaultResolver.LookupHost(ctx, "host.docker.internal")
	cancel()
	fmt.Printf("Step 5 completed in %v, err: %v, addrs: %v\n", time.Since(t0), dnsErr, dockerAddrs)
}
