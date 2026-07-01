//go:build windows

package hoststat

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modiphlpapi        = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetIfEntry2    = modiphlpapi.NewProc("GetIfEntry2")
	modkernel32        = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemTimes = modkernel32.NewProc("GetSystemTimes")
	procGlobalMemory   = modkernel32.NewProc("GlobalMemoryStatusEx")
)

// MIB_IF_ROW2 is ~1352 bytes; we use a byte buffer and read fields at fixed offsets.
const (
	ifRow2Size       = 1400 // generous; the real struct is ~1352 bytes
	offInterfaceIdx  = 8
	offMediaConnect  = 1164 // MediaConnectState: 1 = Connected, 2 = Disconnected
	offTransmitSpeed = 1192
	offInOctets      = 1208
	offInDiscards    = 1232
	offInErrors      = 1240
	offOutOctets     = 1280
	offOutDiscards   = 1304
	offOutErrors     = 1312

	mediaConnected = 1 // MediaConnectStateConnected
)

// platformInterfaceCounters queries GetIfEntry2 for each Go-visible interface.
func platformInterfaceCounters() ([]Iface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []Iface
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		row := make([]byte, ifRow2Size)
		binary.LittleEndian.PutUint32(row[offInterfaceIdx:], uint32(ifc.Index))
		r, _, _ := procGetIfEntry2.Call(uintptr(unsafe.Pointer(&row[0])))
		if r != 0 { // not NO_ERROR
			continue
		}
		out = append(out, Iface{
			Name:      ifc.Name,
			RxBytes:   binary.LittleEndian.Uint64(row[offInOctets:]),
			TxBytes:   binary.LittleEndian.Uint64(row[offOutOctets:]),
			RxErrors:  binary.LittleEndian.Uint64(row[offInErrors:]),
			TxErrors:  binary.LittleEndian.Uint64(row[offOutErrors:]),
			RxDropped: binary.LittleEndian.Uint64(row[offInDiscards:]),
			TxDropped: binary.LittleEndian.Uint64(row[offOutDiscards:]),
			SpeedBps:  binary.LittleEndian.Uint64(row[offTransmitSpeed:]),
			LinkUp:    binary.LittleEndian.Uint32(row[offMediaConnect:]) == mediaConnected,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no interface counters available")
	}
	return out, nil
}

// platformWiFi is not implemented on Windows yet; it soft-fails cleanly. (Linux uses
// /proc/net/wireless; a Windows wlanapi implementation is future work.)
func platformWiFi(string) (*WiFi, error) {
	return nil, fmt.Errorf("wifi metrics not implemented on windows")
}

type filetime struct{ low, high uint32 }

func (f filetime) u64() uint64 { return uint64(f.high)<<32 | uint64(f.low) }

var (
	cpuMu       sync.Mutex
	prevIdle    uint64
	prevBusy    uint64
	prevHasData bool
)

// platformResourcePressure uses GetSystemTimes (CPU) and GlobalMemoryStatusEx (memory).
func platformResourcePressure() (cpu, mem float64, err error) {
	var idle, kernel, user filetime
	r, _, e := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r == 0 {
		return 0, 0, fmt.Errorf("GetSystemTimes: %v", e)
	}
	// kernel time includes idle; busy = (kernel+user) - idle.
	total := kernel.u64() + user.u64()
	idleV := idle.u64()
	busy := total - idleV

	cpuMu.Lock()
	if !prevHasData {
		prevIdle, prevBusy, prevHasData = idleV, busy, true
		cpu = 0
	} else {
		dBusy := busy - prevBusy
		dIdle := idleV - prevIdle
		dTotal := dBusy + dIdle
		prevIdle, prevBusy = idleV, busy
		if dTotal > 0 {
			cpu = float64(dBusy) / float64(dTotal)
		}
	}
	cpuMu.Unlock()

	// MEMORYSTATUSEX: dwLength(4), dwMemoryLoad(4), then several ULONGLONGs.
	var msx [64]byte
	binary.LittleEndian.PutUint32(msx[0:], 64)
	r, _, e = procGlobalMemory.Call(uintptr(unsafe.Pointer(&msx[0])))
	if r == 0 {
		return cpu, 0, fmt.Errorf("GlobalMemoryStatusEx: %v", e)
	}
	load := binary.LittleEndian.Uint32(msx[4:8]) // 0-100
	mem = float64(load) / 100
	return cpu, mem, nil
}
