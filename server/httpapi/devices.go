// Devices: the places collectors run on (a computer with a browser, a phone), and what they report of themselves.
// A device says what collectors it has and how each is doing, as often as it likes; the server keeps the latest
// report in memory and shows it to the owner. It does not schedule, start or stop anything: what a collector
// collects and when is the device's own business. A device that stops reporting is shown as offline.
package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// deviceTTL is how long a report counts as the device being there.
const deviceTTL = 3 * time.Minute

const (
	maxDevices         = 50
	maxDeviceCollector = 50
)

// The report types below are the wire form of internal/device's, written out again on purpose: the server does not
// import the device package, and what a device adds that the server does not know is dropped, not shown.
//
// CollectorReport is what a device says of one collector. The server shows it and reads none of it, so a
// collector can say what it likes in State and Message: ok, running, blocked, login, paused, ...
type CollectorReport struct {
	Name    string `json:"name"`
	Account string `json:"account,omitempty"`
	State   string `json:"state,omitempty"`
	LastRun string `json:"lastRun,omitempty"`
	NextRun string `json:"nextRun,omitempty"`
	Message string `json:"message,omitempty"`
}

// DeviceReport is what a device sends: its name and its collectors.
type DeviceReport struct {
	Device     string            `json:"device"`
	Collectors []CollectorReport `json:"collectors"`
}

// DeviceView is a device as the owner sees it.
type DeviceView struct {
	DeviceReport
	ReportedAt string `json:"reportedAt"`
	Online     bool   `json:"online"`
}

type deviceState struct {
	report DeviceReport
	at     time.Time
}

// Devices keeps the latest report of each device.
type Devices struct {
	mu   sync.Mutex
	now  func() time.Time
	byID map[string]deviceState
}

func NewDevices(now func() time.Time) *Devices {
	if now == nil {
		now = time.Now
	}
	return &Devices{now: now, byID: map[string]deviceState{}}
}

// Report keeps a device's report, and says whether it differs from the one before it (apart from when it was made),
// or the device was offline: what a page showing devices needs to be told about.
func (d *Devices) Report(report DeviceReport) (changed bool) {
	report.Collectors = append([]CollectorReport(nil), report.Collectors...) // the caller may go on changing its own
	d.mu.Lock()
	defer d.mu.Unlock()
	before, known := d.byID[report.Device]
	now := d.now()
	wasOnline := known && now.Sub(before.at) < deviceTTL
	d.byID[report.Device] = deviceState{report: report, at: now}
	if !wasOnline {
		return true
	}
	a, _ := json.Marshal(before.report)
	b, _ := json.Marshal(report)
	return string(a) != string(b)
}

// List is every device that has reported, in name order.
func (d *Devices) List() []DeviceView {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	views := make([]DeviceView, 0, len(d.byID))
	for _, state := range d.byID {
		views = append(views, DeviceView{DeviceReport: state.report, ReportedAt: state.at.UTC().Format(time.RFC3339), Online: now.Sub(state.at) < deviceTTL})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Device < views[j].Device })
	return views
}

// room reports whether the device has reported before, or there is room for another.
func (d *Devices) room(name string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, known := d.byID[name]
	return known || len(d.byID) < maxDevices
}

func validText(s string, limit int) bool {
	return utf8.RuneCountInString(s) <= limit && !strings.ContainsAny(s, "\r\n\x00")
}

// reportDevice takes a device's report of itself. It is a collector's call: it needs the collect token.
func (a *API) reportDevice(w http.ResponseWriter, r *http.Request) {
	body, e := readJSON(w, r)
	if e != nil {
		writeError(w, e)
		return
	}
	var report DeviceReport
	if json.Unmarshal(body, &report) != nil || report.Device == "" || !validText(report.Device, 60) || len(report.Collectors) > maxDeviceCollector {
		writeError(w, badf("device must be a name of at most 60 characters and collectors at most %d", maxDeviceCollector))
		return
	}
	for _, c := range report.Collectors {
		if c.Name == "" || !validText(c.Name, 60) || !validText(c.Account, 120) || !validText(c.State, 30) || !validText(c.LastRun, 40) || !validText(c.NextRun, 40) || !validText(c.Message, 300) {
			writeError(w, badf("each collector needs a name, and short single-line fields"))
			return
		}
	}
	if !a.devices.room(report.Device) {
		writeError(w, badf("too many devices"))
		return
	}
	if a.devices.Report(report) {
		a.hub.Publish(ChangedCollectors)
	}
	w.WriteHeader(http.StatusNoContent)
}

// listDevices is the devices that have reported, for the owner.
func (a *API) listDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"devices": a.devices.List()})
}

// Accounts are the accounts the online devices say their collectors are signed in to or were told to collect:
// the ones the owner may bind as their own.
func (d *Devices) Accounts() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	seen := map[string]bool{}
	for _, state := range d.byID {
		if now.Sub(state.at) >= deviceTTL {
			continue
		}
		for _, c := range state.report.Collectors {
			if c.Account != "" {
				seen[c.Account] = true
			}
		}
	}
	accounts := make([]string, 0, len(seen))
	for id := range seen {
		accounts = append(accounts, id)
	}
	sort.Strings(accounts)
	return accounts
}
