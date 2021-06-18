package manager

import (
	"fmt"
	"io"
	buf "matrixone/pkg/vm/engine/aoe/storage/buffer"
	mgrif "matrixone/pkg/vm/engine/aoe/storage/buffer/manager/iface"
	"matrixone/pkg/vm/engine/aoe/storage/buffer/node"
	nif "matrixone/pkg/vm/engine/aoe/storage/buffer/node/iface"
	w "matrixone/pkg/vm/engine/aoe/storage/worker"
	iw "matrixone/pkg/vm/engine/aoe/storage/worker/base"
	"sync/atomic"
	// log "github.com/sirupsen/logrus"
)

var (
	_                  mgrif.IBufferManager = (*BufferManager)(nil)
	TRANSIENT_START_ID                      = ^(uint64(0)) / 2
)

func NewBufferManager(capacity uint64, flusher iw.IOpWorker, evict_ctx ...interface{}) mgrif.IBufferManager {
	mgr := &BufferManager{
		IMemoryPool:     buf.NewSimpleMemoryPool(capacity),
		Nodes:           make(map[uint64]nif.INodeHandle),
		EvictHolder:     NewSimpleEvictHolder(evict_ctx...),
		NextID:          uint64(0),
		NextTransientID: TRANSIENT_START_ID,
		Flusher:         flusher,
	}

	return mgr
}

func (mgr *BufferManager) NodeCount() int {
	mgr.RLock()
	defer mgr.RUnlock()
	return len(mgr.Nodes)
}

func (mgr *BufferManager) String() string {
	mgr.RLock()
	defer mgr.RUnlock()
	s := fmt.Sprintf("BMgr[Cap:%d, Usage:%d, Nodes:%d, LoadTimes:%d, EvictTimes:%d]:\n", mgr.GetCapacity(), mgr.GetUsage(),
		len(mgr.Nodes), atomic.LoadInt64(&mgr.LoadTimes), atomic.LoadInt64(&mgr.EvictTimes))
	for _, node := range mgr.Nodes {
		s = fmt.Sprintf("%s\n\t%d | %s | Cap: %d ", s, node.GetID(), nif.NodeStateString(mgr.Nodes[node.GetID()].GetState()), mgr.Nodes[node.GetID()].GetCapacity())
	}
	// var mapped = map[uint64]map[uint64][]common.ID{}
	// for k, _ := range mgr.Nodes {
	// 	_, ok := mapped[k.TableID]
	// 	if !ok {
	// 		mapped[k.TableID] = make(map[uint64][]common.ID)
	// 	}
	// 	l := mapped[k.TableID][k.SegmentID]
	// 	l = append(l, k)
	// 	mapped[k.TableID][k.SegmentID] = l
	// }
	// for tbID, segMap := range mapped {
	// 	s += fmt.Sprintf("Table %d SegmentCnt=%d {\n", tbID, len(segMap))
	// 	for segID, ids := range segMap {
	// 		s += fmt.Sprintf("  Segment %d PartCnt=%d {\n", segID, len(ids))
	// 		for _, id := range ids {
	// 			s += fmt.Sprintf("    (Col: %d Blk:%d, Part: %d) [Iter=%d] (%s) (Cap=%d)\n", id.Idx, id.BlockID, id.PartID,
	// 				id.Iter, nif.NodeStateString(mgr.Nodes[id].GetState()), mgr.Nodes[id].GetCapacity())
	// 		}
	// 		s += "  }\n"
	// 	}
	// 	s += "}"
	// }
	// return s
	return s
}

func (mgr *BufferManager) GetNextID() uint64 {
	return atomic.AddUint64(&mgr.NextID, uint64(1)) - 1
}

func (mgr *BufferManager) GetNextTransientID() uint64 {
	return atomic.AddUint64(&mgr.NextTransientID, uint64(1)) - 1
}

func (mgr *BufferManager) RegisterMemory(capacity uint64, spillable bool, constructor buf.MemoryNodeConstructor) nif.INodeHandle {
	pNode := mgr.makePoolNode(capacity, constructor)
	if pNode == nil {
		return nil
	}
	id := mgr.GetNextTransientID()
	ctx := node.NodeHandleCtx{
		ID:          id,
		Manager:     mgr,
		Buff:        node.NewNodeBuffer(id, pNode),
		Spillable:   spillable,
		Constructor: constructor,
	}
	handle := node.NewNodeHandle(&ctx)
	return handle
}

func (mgr *BufferManager) RegisterSpillableNode(capacity uint64, node_id uint64, constructor buf.MemoryNodeConstructor) nif.INodeHandle {
	// log.Infof("RegisterSpillableNode %s", node_id.String())
	{
		mgr.RLock()
		handle, ok := mgr.Nodes[node_id]
		if ok {
			if !handle.IsClosed() {
				mgr.RUnlock()
				return handle
			}
		}
		mgr.RUnlock()
	}

	pNode := mgr.makePoolNode(capacity, constructor)
	if pNode == nil {
		return nil
	}
	ctx := node.NodeHandleCtx{
		ID:          node_id,
		Manager:     mgr,
		Buff:        node.NewNodeBuffer(node_id, pNode),
		Spillable:   true,
		Constructor: constructor,
	}
	handle := node.NewNodeHandle(&ctx)

	mgr.Lock()
	defer mgr.Unlock()
	h, ok := mgr.Nodes[node_id]
	if ok {
		if !h.IsClosed() {
			go func() { pNode.FreeMemory() }()
			return h
		}
	}

	mgr.Nodes[node_id] = handle
	return handle
}

func (mgr *BufferManager) RegisterNode(capacity uint64, node_id uint64, reader io.Reader, constructor buf.MemoryNodeConstructor) nif.INodeHandle {
	mgr.Lock()
	defer mgr.Unlock()
	// log.Infof("RegisterNode %s", node_id.String())

	handle, ok := mgr.Nodes[node_id]
	if ok {
		if !handle.IsClosed() {
			return handle
		}
	}
	ctx := node.NodeHandleCtx{
		ID:          node_id,
		Manager:     mgr,
		Size:        capacity,
		Spillable:   false,
		Reader:      reader,
		Constructor: constructor,
	}
	handle = node.NewNodeHandle(&ctx)
	mgr.Nodes[node_id] = handle
	return handle
}

func (mgr *BufferManager) UnregisterNode(h nif.INodeHandle) {
	node_id := h.GetID()
	// log.Infof("UnRegisterNode %s", node_id.String())
	if h.IsSpillable() {
		if node_id >= TRANSIENT_START_ID {
			h.Clean()
			return
		} else {
			mgr.Lock()
			delete(mgr.Nodes, node_id)
			h.Clean()
			mgr.Unlock()
			return
		}
	}
	mgr.Lock()
	defer mgr.Unlock()
	delete(mgr.Nodes, node_id)
}

func (mgr *BufferManager) Unpin(handle nif.INodeHandle) {
	handle.Lock()
	defer handle.Unlock()
	if !handle.UnRef() {
		panic("logic error")
	}
	if !handle.HasRef() {
		atomic.AddInt64(&mgr.EvictTimes, int64(1))
		evict_node := &EvictNode{Handle: handle, Iter: handle.IncIteration()}
		mgr.EvictHolder.Enqueue(evict_node)
	}
}

func (mgr *BufferManager) makePoolNode(capacity uint64, constructor buf.MemoryNodeConstructor) buf.IMemoryNode {
	node := mgr.Alloc(capacity, constructor)
	if node != nil {
		return node
	}
	for node == nil {
		// log.Printf("makePoolNode capacity %d now %d", capacity, mgr.GetUsageSize())
		evict_node := mgr.EvictHolder.Dequeue()
		// log.Infof("Evict node %s", evict_node.String())
		if evict_node == nil {
			// log.Printf("Cannot get node from queue")
			return nil
		}
		if evict_node.Handle.IsClosed() {
			continue
		}

		if !evict_node.Unloadable(evict_node.Handle) {
			continue
		}

		{
			evict_node.Handle.Lock()
			if !evict_node.Unloadable(evict_node.Handle) {
				evict_node.Handle.Unlock()
				continue
			}
			if !evict_node.Handle.Unloadable() {
				evict_node.Handle.Unlock()
				continue
			}
			evict_node.Handle.Unload()
			evict_node.Handle.Unlock()
		}
		node = mgr.Alloc(capacity, constructor)
	}
	return node
}

func (mgr *BufferManager) Pin(handle nif.INodeHandle) nif.IBufferHandle {
	handle.Lock()
	defer handle.Unlock()
	if handle.PrepareLoad() {
		n := mgr.makePoolNode(handle.GetCapacity(), handle.GetNodeCreator())
		if n == nil {
			handle.RollbackLoad()
			// log.Warnf("Cannot makeSpace(%d,%d)", handle.GetCapacity(), mgr.GetCapacity())
			return nil
		}
		buf := node.NewNodeBuffer(handle.GetID(), n)
		handle.SetBuffer(buf)
		if err := handle.CommitLoad(); err != nil {
			handle.RollbackLoad()
			panic(err.Error())
		}
		atomic.AddInt64(&mgr.LoadTimes, int64(1))
	}
	handle.Ref()
	return handle.MakeHandle()
}

func MockBufMgr(capacity uint64) mgrif.IBufferManager {
	flusher := w.NewOpWorker("MockFlusher")
	return NewBufferManager(capacity, flusher)
}