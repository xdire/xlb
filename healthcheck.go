package xlb

import (
	"container/heap"
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type HealthSchedulerOptions struct {
	MaxItems        int
	Logger          Logger
	ReleaseChecks   int
	CheckIntervalMs int
}

type HealthCheckScheduler struct {
	Q             taskQueue
	mu            sync.Mutex
	ctx           context.Context
	nextId        int64
	isSleeping    int32
	taskAdded     chan int
	logger        Logger
	releaseChecks int
	checkInterval int
}

type healthCheckItem struct {
	exec     func() error
	failures int
	success  int
	route    *route
}

func NewHealthCheckScheduler(opt HealthSchedulerOptions) *HealthCheckScheduler {
	maxItems := 16
	releaseChecks := 1
	checkIntervalMs := 5000
	var logger Logger
	if opt.MaxItems > 0 {
		maxItems = opt.MaxItems
	}
	if opt.Logger == nil {
		logger = newZeroLogForName("xlb-hc", "", "error")
	}
	if opt.ReleaseChecks > 0 {
		releaseChecks = opt.ReleaseChecks
	}
	if opt.CheckIntervalMs > 0 {
		checkIntervalMs = opt.CheckIntervalMs
	}
	return &HealthCheckScheduler{
		Q:             newTaskQueue(maxItems),
		taskAdded:     make(chan int, 2),
		logger:        logger,
		releaseChecks: releaseChecks,
		checkInterval: checkIntervalMs,
	}
}

func (ts *HealthCheckScheduler) AddUnhealthy(ctx context.Context, rte *route, timeout time.Duration) {
	// Mark unhealthy
	rte.healthy.Store(false)
	// Add to the scheduler
	ts.add(&healthCheckItem{func() error {
		dest, err := net.DialTimeout("tcp", rte.address, timeout)
		if err != nil {
			ts.logger.Error(fmt.Sprintf("HC@route unreachable %s", rte.address))
			return err
		}
		err = dest.Close()
		if err != nil {
			return err
		}
		return nil
	}, 0, 0, rte}, int64(ts.checkInterval))

	// Add routine per health issue, as health issues will be resolved routines eventually will
	// wind down until none will be left unless new issue arrived
	go ts.watchRoutine(ctx)

}

func (ts *HealthCheckScheduler) watchRoutine(ctx context.Context) {
	for {
		// Await for the scheduler
		item, err := ts.poll(ctx, 1)
		if err != nil {
			// Context ended return
			return
		}
		// Execute the plan for recovery
		err = item.exec()
		if err != nil {
			item.failures++
			item.success = 0
		} else {
			item.success++
			item.failures = 0
		}
		// Don't check inactive routes, just exit one of the watchers
		if !item.route.active.Load() {
			return
		}
		// Check if recovery matching strategy then exit routine
		if item.success >= ts.releaseChecks {
			item.route.healthy.Store(true)
			return
		}
		// If not ready, reschedule
		// TODO: Add fading mechanics to the consecutive checks due to if we find host healthy, we want to return it back asap
		ts.add(item, int64(ts.checkInterval))
	}
}

func (ts *HealthCheckScheduler) add(task *healthCheckItem, after int64) {
	id := atomic.AddInt64(&ts.nextId, 1)
	item := &taskItem{
		Id:       id,
		Value:    task,
		Priority: time.Now().UTC().Add(time.Duration(after) * time.Millisecond).Unix(),
	}
	ts.logger.Debug(fmt.Sprintf("HC@Offer expiration offered as %d time: %d", after, item.Priority))
	ts.mu.Lock()
	heap.Push(&ts.Q, item)
	ts.mu.Unlock()

	// Check if we actually pushed new element on the top meaning new element is
	// ready for the dispatch
	if item.Index == 0 {
		if atomic.CompareAndSwapInt32(&ts.isSleeping, 1, 0) {
			// Wake up whole pool of workers
			ts.taskAdded <- 1
		}
	}
}

func (ts *HealthCheckScheduler) poll(ctx context.Context, pollerId int) (*healthCheckItem, error) {
	iteratedOnTask := int64(0)
	for {
		isNow := time.Now().UTC().Unix()
		ts.mu.Lock()
		i := ts.Q.Peek()
		iteratedOnTask++
		if i == nil {

			// Set Sleep condition here
			atomic.StoreInt32(&ts.isSleeping, 1)

			ts.mu.Unlock()

			// Wait for semaphore to unlock
			select {
			case <-ts.taskAdded:
				ts.logger.Debug(fmt.Sprintf("HC@Poller <%d> Called out on task added", pollerId))
				break
			case <-ctx.Done():
				return nil, fmt.Errorf("context ended for the Poll action")
			}

			continue

		} else {

			iteratedOnTask++
			task := i.(*taskItem)
			// If task is ready to pop
			if task.Priority <= isNow {
				i = heap.Pop(&ts.Q)
				ts.mu.Unlock()
				ts.logger.Debug(fmt.Sprintf("HC@Poller <%d> poll dequeue id <%d> at <%d> system <%d> loadIter <%d>", pollerId, task.Id, task.Priority, isNow, iteratedOnTask))
				return task.Value.(*healthCheckItem), nil
			} else {
				// If Task should await for the next moment
				ts.mu.Unlock()
				select {
				// Wait for general condition unlock
				case <-ts.taskAdded:
					ts.logger.Debug(fmt.Sprintf("HC@Poller <%d> Called out on task added", pollerId))
					continue
				// Delay next checkup
				case <-time.After(time.Duration(task.Priority-isNow) * time.Millisecond * 1000):
					ts.logger.Debug(fmt.Sprintf("HC@Poller <%d> Called out on time duration block end", pollerId))
					continue
				case <-ctx.Done():
					return nil, fmt.Errorf("context ended for the Poll action")
				}
			}

		}
	}
}

// Object to operate within queue
type taskItem struct {
	Id       int64
	Value    interface{}
	Priority int64
	Index    int
}

// Min Queue
type taskQueue []*taskItem

func newTaskQueue(capacity int) taskQueue {
	return make(taskQueue, 0, capacity)
}

func (pq taskQueue) Len() int {
	return len(pq)
}

func (pq taskQueue) Less(i, j int) bool {
	return pq[i].Priority < pq[j].Priority
}

func (pq taskQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].Index = i
	pq[j].Index = j
}

func (pq *taskQueue) Push(x interface{}) {
	n := len(*pq)
	c := cap(*pq)
	if n+1 > c {
		npq := make(taskQueue, n, c*2)
		copy(npq, *pq)
		*pq = npq
	}
	*pq = (*pq)[0 : n+1]
	item := x.(*taskItem)
	item.Index = n
	(*pq)[n] = item
}

func (pq *taskQueue) Pop() interface{} {
	n := len(*pq)
	c := cap(*pq)
	if n < (c/2) && c > 25 {
		npq := make(taskQueue, n, c/2)
		copy(npq, *pq)
		*pq = npq
	}
	item := (*pq)[n-1]
	item.Index = -1
	*pq = (*pq)[0 : n-1]
	return item
}

func (pq taskQueue) Peek() interface{} {
	if pq.Len() == 0 {
		return nil
	}
	return pq[0]
}
