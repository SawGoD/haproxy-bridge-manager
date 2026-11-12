package monitor

import (
	"context"
	"testing"
)

func TestUnknown(t *testing.T) {
	if Active(nil, "b") != "UNKNOWN" {
		t.Fatal("invented active server")
	}
	if _, e := Read(context.Background(), ""); e == nil {
		t.Fatal("expected unavailable")
	}
}
func TestStats(t *testing.T) {
	s, e := Parse("# pxname,svname,status,scur,stot,econ,check_status,lastchg,bck,\nb,s1,UP,2,20,1,L4OK,3,0,\nb,s2,DOWN,0,1,0,L4CON,4,1,\n")
	if e != nil || len(s) != 2 {
		t.Fatalf("%v %v", s, e)
	}
	if Active(s, "b") != "s1" || s[0].Total != 20 || !s[1].Backup {
		t.Fatal(s)
	}
}
