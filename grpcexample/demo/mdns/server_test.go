package mdnsdemo

import (
	"github.com/micro/mdns"
	"sync/atomic"
	"testing"
	"time"
)

func TestServer_StartStop(t *testing.T) {
	s := makeService(t)
	serv, err := mdns.NewServer(&mdns.Config{Zone: s})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer serv.Shutdown()
}

func TestServer_Lookup(t *testing.T) {
	serv, err := mdns.NewServer(&mdns.Config{Zone: makeServiceWithServiceName(t, "_foobar._tcp")})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer serv.Shutdown()

	entries := make(chan *mdns.ServiceEntry, 1)
	var found int32 = 0
	go func() {
		select {
		case e := <-entries:
			if e.Name != "hostname._foobar._tcp.local." {
				t.Fatalf("bad: %v", e)
			}
			if e.Port != 80 {
				t.Fatalf("bad: %v", e)
			}
			if e.Info != "Local web server" {
				t.Fatalf("bad: %v", e)
			}
			atomic.StoreInt32(&found, 1)

		case <-time.After(80 * time.Millisecond):
			t.Fatalf("timeout")
		}
	}()

	params := &mdns.QueryParam{
		Service: "_foobar._tcp",
		Domain:  "local",
		Timeout: 50 * time.Millisecond,
		Entries: entries,
	}
	err = mdns.Query(params)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if atomic.LoadInt32(&found) == 0 {
		t.Fatalf("record not found")
	}
}
