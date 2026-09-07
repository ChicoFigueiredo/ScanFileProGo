//go:build windows

package watcher

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Completion keys used to tell a finished directory read apart from the wake-up
// packet posted by Stop.
const (
	readCompletionKey uintptr = 1
	stopCompletionKey uintptr = 2
)

// notifyFilter covers everything that can change the tree: names (create,
// delete, rename), sizes, contents and creation timestamps.
const notifyFilter = windows.FILE_NOTIFY_CHANGE_FILE_NAME |
	windows.FILE_NOTIFY_CHANGE_DIR_NAME |
	windows.FILE_NOTIFY_CHANGE_SIZE |
	windows.FILE_NOTIFY_CHANGE_LAST_WRITE |
	windows.FILE_NOTIFY_CHANGE_CREATION

// minBufferSize is the smallest notification buffer accepted; smaller buffers
// cannot hold a single record and would overflow forever.
const minBufferSize = 64

// rootWatcher owns one recursive ReadDirectoryChangesW subscription.
//
// The struct is always heap allocated (the returned stop closure captures it),
// which matters because the kernel writes into buf and ov while the read is
// pending and Go stacks may move.
type rootWatcher struct {
	root   string
	handle windows.Handle
	iocp   windows.Handle
	ov     windows.Overlapped
	buf    []byte
	sink   changeSink

	stopped  atomic.Bool
	stopOnce sync.Once
	done     chan struct{}
}

// startRootWatch watches root recursively with ReadDirectoryChangesW and
// overlapped I/O, in one goroutine. The returned function cancels the pending
// read and only returns once that goroutine is gone.
func startRootWatch(root string, bufSize int, sink changeSink) (func(), error) {
	if bufSize < minBufferSize {
		bufSize = minBufferSize
	}
	bufSize = (bufSize + 3) &^ 3 // FILE_NOTIFY_INFORMATION requires DWORD alignment

	pathPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return nil, fmt.Errorf("caminho inválido para monitoramento %q: %w", root, err)
	}

	handle, err := windows.CreateFile(
		pathPtr,
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("não foi possível abrir %q para monitoramento: %w", root, err)
	}

	iocp, err := windows.CreateIoCompletionPort(handle, 0, readCompletionKey, 1)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("não foi possível criar a porta de conclusão para %q: %w", root, err)
	}

	rw := &rootWatcher{
		root:   root,
		handle: handle,
		iocp:   iocp,
		buf:    make([]byte, bufSize),
		sink:   sink,
		done:   make(chan struct{}),
	}

	// The first read is armed here, not in the goroutine, so that any change
	// made after startRootWatch returns is guaranteed to be observed.
	if err := rw.arm(); err != nil {
		_ = windows.CloseHandle(handle)
		_ = windows.CloseHandle(iocp)
		return nil, fmt.Errorf("não foi possível iniciar a leitura de mudanças de %q: %w", root, err)
	}

	go rw.loop()
	return rw.stop, nil
}

// arm issues one overlapped ReadDirectoryChangesW over the whole subtree.
func (rw *rootWatcher) arm() error {
	return windows.ReadDirectoryChanges(
		rw.handle,
		&rw.buf[0],
		uint32(len(rw.buf)),
		true, // bWatchSubtree: recursive, the whole Raiz Varrida
		notifyFilter,
		nil,
		&rw.ov,
		0,
	)
}

// loop consumes one completion after another and re-arms the read until stop.
func (rw *rootWatcher) loop() {
	readPending := true // armed by startRootWatch

	defer func() {
		// A read may still be armed if we woke up on the stop packet. Cancel it
		// and drain its completion before the buffer becomes garbage.
		if readPending {
			_ = windows.CancelIoEx(rw.handle, &rw.ov)
			var qty uint32
			var key uintptr
			var ov *windows.Overlapped
			if err := windows.GetQueuedCompletionStatus(rw.iocp, &qty, &key, &ov, windows.INFINITE); err == nil && key == stopCompletionKey {
				// Drained the stop packet instead; the aborted read is next.
				_ = windows.GetQueuedCompletionStatus(rw.iocp, &qty, &key, &ov, 5000)
			}
		}
		runtime.KeepAlive(rw.buf)
		close(rw.done)
	}()

	for {
		var qty uint32
		var key uintptr
		var ov *windows.Overlapped
		err := windows.GetQueuedCompletionStatus(rw.iocp, &qty, &key, &ov, windows.INFINITE)
		if key == stopCompletionKey {
			return
		}
		readPending = false

		if rw.stopped.Load() {
			return
		}

		switch {
		case err == nil && qty > 0:
			rw.parse(qty)
		case err == nil && qty == 0, errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR):
			// The kernel dropped notifications: the caller must re-scan the root.
			if rw.sink.Overflow != nil {
				rw.sink.Overflow()
			}
		case errors.Is(err, windows.ERROR_OPERATION_ABORTED):
			return
		default:
			return
		}

		if rw.stopped.Load() {
			return
		}
		if err := rw.arm(); err != nil {
			return
		}
		readPending = true
	}
}

// parse walks the FILE_NOTIFY_INFORMATION records written by the kernel.
func (rw *rootWatcher) parse(qty uint32) {
	limit := uint32(len(rw.buf))
	if qty < limit {
		limit = qty
	}

	var offset uint32
	for offset+uint32(unsafe.Sizeof(windows.FileNotifyInformation{})) <= limit {
		raw := (*windows.FileNotifyInformation)(unsafe.Pointer(&rw.buf[offset]))

		nameLen := raw.FileNameLength / 2
		nameStart := offset + uint32(unsafe.Offsetof(raw.FileName))
		if nameStart+raw.FileNameLength > limit {
			// Truncated record: treat the rest of the buffer as lost.
			if rw.sink.Overflow != nil {
				rw.sink.Overflow()
			}
			return
		}

		if nameLen > 0 && rw.sink.Change != nil {
			name := windows.UTF16ToString(unsafe.Slice(&raw.FileName, nameLen))
			renamed := raw.Action == windows.FILE_ACTION_RENAMED_OLD_NAME ||
				raw.Action == windows.FILE_ACTION_RENAMED_NEW_NAME
			rw.sink.Change(filepath.Join(rw.root, name), renamed)
		}

		if raw.NextEntryOffset == 0 {
			return
		}
		next := offset + raw.NextEntryOffset
		if next <= offset || next >= limit {
			return
		}
		offset = next
	}
}

// stop cancels the pending read, waits for the goroutine and closes the handles.
func (rw *rootWatcher) stop() {
	rw.stopOnce.Do(func() {
		rw.stopped.Store(true)
		_ = windows.CancelIoEx(rw.handle, &rw.ov)
		_ = windows.PostQueuedCompletionStatus(rw.iocp, 0, stopCompletionKey, nil)
		<-rw.done
		_ = windows.CloseHandle(rw.handle)
		_ = windows.CloseHandle(rw.iocp)
	})
}
