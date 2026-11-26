package httpapi

import (
	"testing"
	"time"
)

func TestADeviceIsOnlineWhileItKeepsReporting(t *testing.T) {
	clock := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	devices := NewDevices(func() time.Time { return clock })
	report := DeviceReport{Device: "desk", Collectors: []CollectorReport{{Name: "weibo", State: "ok"}}}

	if !devices.Report(report) {
		t.Error("the first report of a device is news")
	}
	clock = clock.Add(time.Minute)
	if devices.Report(report) {
		t.Error("the same report a minute later is not news")
	}
	report.Collectors[0].State = "blocked"
	if !devices.Report(report) {
		t.Error("a collector that is blocked now is news")
	}
	if list := devices.List(); len(list) != 1 || !list[0].Online || list[0].Collectors[0].State != "blocked" {
		t.Fatalf("devices = %+v", list)
	}

	clock = clock.Add(deviceTTL + time.Second)
	if list := devices.List(); len(list) != 1 || list[0].Online {
		t.Errorf("a device that stopped reporting is still online: %+v", list)
	}
	if !devices.Report(report) {
		t.Error("a device that comes back is news, even if it says the same")
	}
}

func TestDevicesAreListedByName(t *testing.T) {
	devices := NewDevices(nil)
	for _, name := range []string{"phone", "desk", "laptop"} {
		devices.Report(DeviceReport{Device: name})
	}
	var names []string
	for _, view := range devices.List() {
		names = append(names, view.Device)
	}
	if len(names) != 3 || names[0] != "desk" || names[1] != "laptop" || names[2] != "phone" {
		t.Errorf("devices = %v", names)
	}
}
