// tracker.go
package zavax

import ( 
	"sync" 
	"fmt"
)

// RequestTracker represents the state of processing requests.
type RequestTracker struct {
    mutex               sync.Mutex
    processingRequests map[uint64]chan struct{}
}

// NewRequestTracker creates a new RequestTracker.
func NewRequestTracker() *RequestTracker {
	fmt.Printf("Tracker initialized\n")
    return &RequestTracker{
        processingRequests: make(map[uint64]chan struct{}),
    }
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
	rt.mutex.Lock()
	defer rt.mutex.Unlock()
	fmt.Printf("Set complete processing 1\n")
	// Signal completion and remove from processing set
	if done, exists := rt.processingRequests[id]; exists {
		close(done)
		delete(rt.processingRequests, id)
		fmt.Printf("Set complete processing %v\n",rt.processingRequests[id])
	}
	fmt.Printf("Set complete processing 2\n")
}