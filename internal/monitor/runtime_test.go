package monitor

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestUnknown(t *testing.T) {
	if len(Active(nil)) != 0 {
		t.Fatal("invented active server")
	}
	if _, e := Read(context.Background(), ""); e == nil {
		t.Fatal("expected unavailable")
	}
}
func TestStats(t *testing.T) {
	s, e := Parse(`# pxname,svname,status,scur,stot,econ,check_status,lastchg,act,bck,type,downtime,check_duration,last_chk
f,FRONTEND,OPEN,2,20,0,,,,,0,,,
f,socket,OPEN,2,20,0,,,,,3,,,
b,main,DOWN,0,20,1,L4CON,4,0,0,2,6,14,Connection refused
b,standby,UP,2,20,0,L4OK,3,0,1,2,0,0,
b,BACKEND,UP,2,40,1,,90,0,1,1,12,,
c,BACKEND,UP,0,0,0,,,1,1,1,0,,
d,BACKEND,DOWN,0,0,0,,,0,0,1,0,,
e,BACKEND,UP,0,0,0,,,,1,1,0,,
`)
	servers := s.Servers["b"]
	if e != nil || len(servers) != 2 || len(s.Servers["f"]) != 0 {
		t.Fatalf("%v %v", s, e)
	}
	if !slices.Equal(Active(servers), []string{"standby"}) || servers[0].Total != 20 || !servers[1].Backup ||
		servers[0].CheckDetail != "Connection refused" || *servers[0].CheckDuration != 14*time.Millisecond ||
		servers[1].CheckDuration == nil || *servers[1].CheckDuration != 0 {
		t.Fatal(s)
	}
	backend := s.Backends["b"]
	if !backend.OnBackup() || *backend.LastChange != 90*time.Second || *backend.Downtime != 12*time.Second {
		t.Fatal(backend)
	}
	for _, name := range []string{"c", "d", "e", "unknown"} {
		if s.Backends[name].OnBackup() || s.Backends[name].LastChange != nil {
			t.Fatal(name, s.Backends[name])
		}
	}
}
