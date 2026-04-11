package snowflake

import (
	"errors"
	"sync"
	"time"
)

const (
	epoch         int64 = 1_767_225_600_000 //2026-01-01
	nodeBits      uint8 = 10
	stepBits      uint8 = 12
	nodeMax       int64 = -1 ^ (-1 << nodeBits) //1023
	stepMax       int64 = -1 ^ (-1 << stepBits) //4095
	timeShift     uint8 = nodeBits + stepBits   //22
	nodeShift     uint8 = stepBits              //12
	maxBackwardMs int64 = 5                     //5ms
)

type Node struct {
	mu        sync.Mutex
	timestamp int64
	nodeID    int64
	step      int64
}

func NewNode(nodeID int64) (*Node, error) {
	if nodeID < 0 || nodeID > nodeMax {
		return nil, errors.New("node ID out of range")
	}
	return &Node{
		nodeID: nodeID,
	}, nil
}

func (n *Node) Generate() (int64, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now().UnixMilli()
	if now < n.timestamp {
		backwardDuration := n.timestamp - now
		if backwardDuration <= maxBackwardMs {
			time.Sleep(time.Duration(backwardDuration) * time.Millisecond)
			now = time.Now().UnixMilli()
		} else {
			return 0, errors.New("clock moved backwards too much")
		}
	} else if now == n.timestamp {
		n.step = (n.step + 1) & stepMax
		if n.step == 0 {
			for now <= n.timestamp {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		n.step = 0
	}
	n.timestamp = now
	id := ((now - epoch) << timeShift) |
		(n.nodeID << nodeShift) |
		(n.step)
	return id, nil
}
