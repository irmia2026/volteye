//go:build windows

package wechatdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modkernel32        = windows.NewLazySystemDLL("kernel32.dll")
	procVirtualQueryEx = modkernel32.NewProc("VirtualQueryEx")
	procReadMemory     = modkernel32.NewProc("ReadProcessMemory")
)

const (
	memCommit  = 0x1000
	memPrivate = 0x20000
	scanChunk  = 64 << 20
	stubLen    = 32
)

type memBasicInfo struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	PartitionID       uint16
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
}

func FindWeChatPID() (uint32, string, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, "", err
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return 0, "", err
	}
	for {
		name := windows.UTF16ToString(pe.ExeFile[:])
		lower := strings.ToLower(name)
		if lower == "weixin.exe" || lower == "wechat.exe" {
			return pe.ProcessID, name, nil
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	return 0, "", errors.New("Weixin.exe process not found (is WeChat running and logged in?)")
}

func readProcessMemory(h windows.Handle, addr uintptr, size int) ([]byte, error) {
	if size <= 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	var n uintptr
	ret, _, err := procReadMemory.Call(
		uintptr(h), addr,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(size),
		uintptr(unsafe.Pointer(&n)),
	)
	if ret == 0 {
		return nil, err
	}
	return buf[:n], nil
}

func queryRegions(h windows.Handle) [][2]uintptr {
	var regions [][2]uintptr
	var addr uintptr
	for {
		var mbi memBasicInfo
		ret, _, _ := procVirtualQueryEx.Call(
			uintptr(h), addr,
			uintptr(unsafe.Pointer(&mbi)),
			unsafe.Sizeof(mbi),
		)
		if ret == 0 {
			break
		}
		if mbi.State == memCommit && mbi.Type == memPrivate && mbi.RegionSize > 0 {
			regions = append(regions, [2]uintptr{mbi.BaseAddress, mbi.RegionSize})
		}
		next := mbi.BaseAddress + mbi.RegionSize
		if next <= addr {
			break
		}
		addr = next
	}
	return regions
}

func matchStubPointers(buf []byte) []uintptr {
	var ptrs []uintptr
	for i := 0; i+stubLen <= len(buf); i++ {
		if buf[i+16] != 0x20 || buf[i+24] != 0x2f {
			continue
		}
		if binary.LittleEndian.Uint64(buf[i+6:]) != 0 || buf[i+14] != 0 || buf[i+15] != 0 {
			continue
		}
		if binary.LittleEndian.Uint32(buf[i+17:]) != 0 || buf[i+21] != 0 || buf[i+22] != 0 || buf[i+23] != 0 {
			continue
		}
		if binary.LittleEndian.Uint32(buf[i+25:]) != 0 || buf[i+29] != 0 || buf[i+30] != 0 || buf[i+31] != 0 {
			continue
		}
		ptr := binary.LittleEndian.Uint64(buf[i:])
		if ptr > 0x10000 && ptr < 0x7fffffffffff {
			ptrs = append(ptrs, uintptr(ptr))
		}
	}
	return ptrs
}

func isPotentialKey(k []byte) bool {
	if len(k) != KeySize {
		return false
	}
	var seen [256]bool
	distinct := 0
	printable := 0
	for _, b := range k {
		if !seen[b] {
			seen[b] = true
			distinct++
		}
		if b >= 32 && b <= 126 {
			printable++
		}
	}
	return distinct >= 15 && printable <= 24
}

func RecoverKey(pid uint32, page1 []byte, logf func(string)) ([]byte, error) {
	h, err := windows.OpenProcess(windows.PROCESS_VM_READ|windows.PROCESS_QUERY_INFORMATION, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess failed: %v (try running as administrator)", err)
	}
	defer windows.CloseHandle(h)

	regions := queryRegions(h)
	if len(regions) == 0 {
		return nil, errors.New("no readable memory regions (try running as administrator)")
	}
	logf(fmt.Sprintf("scanning %d memory regions", len(regions)))

	ptrSet := map[uintptr]struct{}{}
	for ri, rg := range regions {
		base, size := rg[0], rg[1]
		for off := uintptr(0); off < size; off += scanChunk {
			n := uintptr(scanChunk + stubLen)
			if off+n > size {
				n = size - off
			}
			buf, err := readProcessMemory(h, base+off, int(n))
			if err != nil {
				break
			}
			for _, p := range matchStubPointers(buf) {
				ptrSet[p] = struct{}{}
			}
			if len(buf) < int(n) {
				break
			}
		}
		if (ri+1)%200 == 0 {
			logf(fmt.Sprintf("regions %d/%d, stub pointers %d", ri+1, len(regions), len(ptrSet)))
		}
	}
	logf(fmt.Sprintf("stub pointers: %d", len(ptrSet)))

	candSet := map[[KeySize]byte]struct{}{}
	for p := range ptrSet {
		b, err := readProcessMemory(h, p, KeySize)
		if err != nil || len(b) != KeySize {
			continue
		}
		if !isPotentialKey(b) {
			continue
		}
		var k [KeySize]byte
		copy(k[:], b)
		candSet[k] = struct{}{}
	}
	logf(fmt.Sprintf("candidates after entropy filter: %d", len(candSet)))
	if len(candSet) == 0 {
		return nil, errors.New("no key candidates found in process memory")
	}

	cands := make([][]byte, 0, len(candSet))
	for k := range candSet {
		kk := make([]byte, KeySize)
		copy(kk, k[:])
		cands = append(cands, kk)
	}

	var (
		wg      sync.WaitGroup
		found   []byte
		foundMu sync.Mutex
		stop    atomic.Bool
		tested  atomic.Int64
	)
	sem := make(chan struct{}, runtime.NumCPU())
	for _, c := range cands {
		wg.Add(1)
		go func(cand []byte) {
			defer wg.Done()
			if stop.Load() {
				return
			}
			sem <- struct{}{}
			defer func() { <-sem }()
			if VerifyPage1Key(cand, page1) {
				foundMu.Lock()
				found = cand
				foundMu.Unlock()
				stop.Store(true)
			}
			tested.Add(1)
		}(c)
	}
	wg.Wait()
	if found == nil {
		return nil, fmt.Errorf("no valid key among %d candidates", len(cands))
	}
	return found, nil
}
