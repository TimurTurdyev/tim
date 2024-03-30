package sysinfo

import "testing"

func TestParseBootAndLoad(t *testing.T) {
	b, ok := ParseBoottime("{ sec = 1759603284, usec = 268414 } Sat Oct  4 21:41:24 2025")
	if !ok || b.Unix() != 1759603284 {
		t.Errorf("boot %v", b)
	}
	if l := ParseLoadavg("{ 2,12 1,85 1,39 }"); len(l) != 3 || l[0] != 2.12 {
		t.Errorf("load comma %v", l)
	}
	if l := ParseLoadavg("{ 0.50 0.40 0.30 }"); len(l) != 3 || l[2] != 0.30 {
		t.Errorf("load dot %v", l)
	}
}

func TestParseVMStat(t *testing.T) {
	out := "Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free:  22409.\nPages active:  100.\nPages inactive:  999.\nPages wired down:  50.\nPages occupied by compressor:  10.\n"
	if got := ParseVMStat(out); got != 160*16384 {
		t.Errorf("used %d", got)
	}
}

func TestParseDFAndPmset(t *testing.T) {
	d, ok := ParseDF("Filesystem 1024-blocks Used Available Capacity iused ifree %iused Mounted on\n/dev/disk3s5 971298980 767109096 132900328 86% 1 2 0% /System/Volumes/Data\n")
	if !ok || d.Percent() != 78 || d.Size != 971298980*1024 {
		t.Errorf("df %+v %d", d, d.Percent())
	}
	b, ok := ParsePmset("Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t87%; discharging; 4:12 remaining present: true\n")
	if !ok || b.Percent != 87 || b.State != "discharging" || b.Remaining != "4:12" || b.AC {
		t.Errorf("batt %+v", b)
	}
	b, _ = ParsePmset("Now drawing from 'AC Power'\n -InternalBattery-0 (id=1)\t100%; charged; 0:00 remaining present: true\n")
	if !b.AC || b.State != "charged" {
		t.Errorf("ac %+v", b)
	}
	if _, ok := ParsePmset("Now drawing from 'AC Power'\n"); ok {
		t.Error("desktop without battery")
	}
}

func TestParseWifi(t *testing.T) {
	data := []byte(`{"SPAirPortDataType":[{"spairport_airport_interfaces":[
	{"_name":"en0","spairport_current_network_information":{"_name":"Home","spairport_network_channel":"36 (5GHz, 160MHz)",
	"spairport_network_phymode":"802.11ax","spairport_network_rate":432,"spairport_security_mode":"spairport_security_mode_wpa2_personal",
	"spairport_signal_noise":"-73 dBm / -97 dBm"}},
	{"_name":"awdl0"}]}]}`)
	w, ok := ParseWifi(data)
	if !ok || w.SSID != "Home" || w.Iface != "en0" || w.Rate != 432 || w.Signal != -73 || w.Noise != -97 || w.Security != "wpa2 personal" {
		t.Fatalf("%+v", w)
	}
	if w.Quality() != "слабый" {
		t.Errorf("quality %s", w.Quality())
	}
	if _, ok := ParseWifi([]byte(`{"SPAirPortDataType":[{"spairport_airport_interfaces":[{"_name":"en0"}]}]}`)); ok {
		t.Error("not connected")
	}
}
