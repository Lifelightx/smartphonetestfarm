package adb

import (
	"context"
	"net"

	"protean-provider/internal/domain"
)

// StreamFromConnForTest is a test seam that exposes the internal stream()
// method for unit testing with a pre-connected net.Conn (e.g. net.Pipe()).
//
// Production code never calls this — it is exported only for the _test package.
// The property fetcher is bypassed (shellClient is nil) so EventConnected events
// will have a nil Device, which is acceptable for tracker-logic tests.
func StreamFromConnForTest(
	ctx context.Context,
	conn net.Conn,
	ch chan<- domain.DeviceEvent,
	previous map[string]string,
) error {
	t := &Tracker{
		inner:       nil, // not needed: propTimeout=0 skips FetchProperties
		propTimeout: 0,
		backoffMin:  0,
		backoffMax:  0,
	}
	return t.stream(ctx, conn, nil, previous, ch)
}
