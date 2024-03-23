// tracker.go
package zavax

import ( 
	"sync" 
	"fmt"
	"time"
)

// RequestTracker represents the state of processing requests.
type RequestTracker struct {
    mutex               sync.Mutex
    processingRequests map[uint64]chan struct{}
	lastResetTime     time.Time
}

// NewRequestTracker creates a new RequestTracker.
func NewRequestTracker() *RequestTracker {
	fmt.Printf("Tracker initialized\n")
    return &RequestTracker{
        processingRequests: make(map[uint64]chan struct{}),
		lastResetTime:     time.Now(),
    
    }
}

func (rt *RequestTracker) shouldReset() bool {
    rt.mutex.Lock()
    defer rt.mutex.Unlock()

    return time.Since(rt.lastResetTime) >= 24*time.Hour
}

func (rt *RequestTracker) resetProcessingRequests() {
    rt.mutex.Lock()
    defer rt.mutex.Unlock()

    // Reset the processingRequests map
    rt.processingRequests = make(map[uint64]chan struct{})
    // Update the last reset time
    rt.lastResetTime = time.Now()
}

// MarkProcessing marks the request ID as currently being processed.
func (rt *RequestTracker) MarkProcessing(id uint64) {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()

	fmt.Printf("Set mark processing\n")
	// Create a channel to signal completion
	done := make(chan struct{})
	rt.processingRequests[id] = done
	fmt.Printf("Set mark processing %v %d\n",rt.processingRequests[id], id)
}

// MarkProcessing marks the request ID as currently being processed.
func (rt *RequestTracker) IsProcessing(id uint64) chan struct{} {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()

	fmt.Printf("check is processing %v %d\n", rt.processingRequests[id], id)
	return rt.processingRequests[id]
}

// CompleteProcessing marks the request ID as completed and removes it from the processing set.
func (rt *RequestTracker) CompleteProcessing(id uint64) {
	if rt.shouldReset() {
        rt.resetProcessingRequests()
    }

	rt.mutex.Lock()
	defer rt.mutex.Unlock()

}