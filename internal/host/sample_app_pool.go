package host

import (
	"fmt"
	"sync"

	"github.com/jnnngs/3270Web/internal/sampleapps"
)

// One listener per bundled sample app, shared by everybody using it.
//
// A sample app is a listener this process opens on a fixed local port, and
// every terminal session pointed at that preset used to open its own. The
// second session for the same app therefore tried to bind a port the first
// one was already holding and failed with an address-in-use error — on a
// preset that is offered to a whole group, which is precisely the one several
// people open at once. The server underneath was never the problem: it accepts
// as many connections as arrive and hands each its own screen.
//
// So the listener is a process-level resource with a reference count rather
// than something a session owns. The first session for a port starts it, the
// rest join it, and it is closed by whichever session happens to be the last
// to leave.
//
// The port is the key because the port is what collides. A second app claiming
// a port another one is already serving is refused out loud: silently handing
// back the wrong server would connect somebody to an application that is not
// the one their preset names, which is worse than the error it replaced.

type sampleAppLease struct {
	appID  string
	server *sampleapps.Server
	refs   int
}

var (
	sampleAppPoolMu sync.Mutex
	sampleAppPool   = map[int]*sampleAppLease{}
)

// acquireSampleAppServer returns the listener for this app and port, starting
// one if nobody is using it yet, and counts the caller against it.
//
// Every successful call must be paired with releaseSampleAppServer, including
// on the paths that fail after it — see GoSampleAppHost.Start.
func acquireSampleAppServer(appID string, port int) (*sampleapps.Server, error) {
	sampleAppPoolMu.Lock()
	defer sampleAppPoolMu.Unlock()

	if lease, ok := sampleAppPool[port]; ok {
		if lease.appID != appID {
			return nil, fmt.Errorf("port %d is already serving the sample app %q, so %q cannot use it",
				port, lease.appID, appID)
		}
		lease.refs++
		return lease.server, nil
	}

	server, err := sampleapps.StartServer(appID, port)
	if err != nil {
		return nil, err
	}
	sampleAppPool[port] = &sampleAppLease{appID: appID, server: server, refs: 1}
	return server, nil
}

// releaseSampleAppServer gives up one caller's claim, stopping the listener
// once the last of them has gone.
//
// Releasing something that is not in the pool — a second Stop, or a server
// this process no longer owns — does nothing rather than closing whatever is
// on that port now.
func releaseSampleAppServer(port int, server *sampleapps.Server) {
	if server == nil {
		return
	}
	sampleAppPoolMu.Lock()
	defer sampleAppPoolMu.Unlock()

	lease, ok := sampleAppPool[port]
	if !ok || lease.server != server {
		return
	}
	lease.refs--
	if lease.refs > 0 {
		return
	}
	delete(sampleAppPool, port)
	_ = lease.server.Stop()
}

// sampleAppRefs reports how many callers hold the listener on a port, for
// tests that need to see the counting rather than infer it.
func sampleAppRefs(port int) int {
	sampleAppPoolMu.Lock()
	defer sampleAppPoolMu.Unlock()
	if lease, ok := sampleAppPool[port]; ok {
		return lease.refs
	}
	return 0
}
